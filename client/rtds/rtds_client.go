package rtds

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/lw396/polymarket-go/client/config"
	"github.com/lw396/polymarket-go/client/endpoint"
)

// RtdsClientOptions configures the RTDS WebSocket client.
type RtdsClientOptions struct {
	AutoReconnect        bool
	ReconnectDelay       time.Duration
	MaxReconnectAttempts int
	PingInterval         time.Duration
	ProxyUrl             string
	Logger               *log.Logger
}

// RtdsCallbacks holds callback functions for RTDS events.
type RtdsCallbacks struct {
	OnCryptoPrice func(payload *CryptoPricePayload)
	OnError       func(err error)
	OnConnect     func()
	OnDisconnect  func(code int, reason string)
	OnReconnect   func(attempt int)
}

// RtdsClient is a WebSocket client for the Polymarket RTDS service.
type RtdsClient struct {
	mu                sync.RWMutex
	writeMu           sync.Mutex
	reconnectMu       sync.Mutex
	closedOnce        sync.Once
	options           *RtdsClientOptions
	callbacks         *RtdsCallbacks
	conn              *websocket.Conn
	pingTicker        *time.Ticker
	reconnectTimer    *time.Timer
	done              chan struct{}
	reconnectAttempts int
	isConnecting      bool
	shouldReconnect   bool
	logger            *log.Logger
}

// NewRtdsClient creates a new RTDS WebSocket client.
func NewRtdsClient(options *RtdsClientOptions) *RtdsClient {
	if options == nil {
		options = &RtdsClientOptions{}
	}
	if options.AutoReconnect && options.ReconnectDelay == 0 {
		options.ReconnectDelay = 5 * time.Second
	}
	if options.PingInterval == 0 {
		options.PingInterval = config.GetRtdsPingInterval()
	}
	logger := options.Logger
	if logger == nil {
		logger = log.Default()
	}
	return &RtdsClient{
		options:         options,
		callbacks:       &RtdsCallbacks{},
		shouldReconnect: true,
		logger:          logger,
	}
}

// On sets the callback handlers. Returns the client for chaining.
func (c *RtdsClient) On(callbacks *RtdsCallbacks) *RtdsClient {
	c.callbacks = callbacks
	return c
}

// Connect establishes a WebSocket connection to the RTDS endpoint.
func (c *RtdsClient) Connect() error {
	c.mu.Lock()
	if c.isConnecting || (c.conn != nil && c.IsConnected()) {
		c.mu.Unlock()
		return nil
	}
	c.isConnecting = true
	c.shouldReconnect = true
	c.mu.Unlock()

	fullURL := endpoint.RtdsWsUrl
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"http/1.1"},
	}
	dialer := websocket.Dialer{TLSClientConfig: tlsConfig}
	if c.options.ProxyUrl != "" {
		proxyUrl, err := url.Parse(c.options.ProxyUrl)
		if err != nil {
			c.mu.Lock()
			c.isConnecting = false
			c.mu.Unlock()
			return fmt.Errorf("failed to parse proxy url: %w", err)
		}
		dialer.Proxy = http.ProxyURL(proxyUrl)
	}

	conn, _, err := dialer.Dial(fullURL, nil)
	if err != nil {
		c.mu.Lock()
		c.isConnecting = false
		c.mu.Unlock()
		return fmt.Errorf("failed to connect to RTDS WebSocket: %w", err)
	}

	c.mu.Lock()
	c.conn = conn
	c.isConnecting = false
	c.reconnectAttempts = 0
	c.done = make(chan struct{})
	c.closedOnce = sync.Once{}
	c.mu.Unlock()

	conn.SetPongHandler(func(appData string) error {
		return nil
	})
	conn.SetPingHandler(func(appData string) error {
		_ = c.withConnWrite(func(c *websocket.Conn) error {
			return c.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(5*time.Second))
		})
		return nil
	})

	go c.handleMessages()
	go c.pingWorker()

	if c.callbacks.OnConnect != nil {
		c.callbacks.OnConnect()
	}
	return nil
}

// Disconnect closes the WebSocket connection.
func (c *RtdsClient) Disconnect() {
	c.mu.Lock()
	c.shouldReconnect = false
	c.mu.Unlock()

	c.cleanup()
	c.forceCloseWithReason(websocket.CloseNormalClosure, "client disconnect")
	c.signalDone()
}

// IsConnected returns whether the client has an active WebSocket connection.
func (c *RtdsClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.conn != nil
}

// Wait blocks until the client is fully disconnected.
func (c *RtdsClient) Wait() {
	c.mu.RLock()
	done := c.done
	c.mu.RUnlock()
	if done != nil {
		<-done
	}
}

func joinSymbolFilters(symbols []string) string {
	if len(symbols) == 0 {
		return ""
	}
	return strings.Join(symbols, ",")
}

func (c *RtdsClient) SubscribeBinance(symbols []string) error {
	return c.sendSubscription(ActionSubscribe, Subscription{
		Topic:   TopicCryptoPrices,
		Type:    SubscriptionTypeUpdate,
		Filters: joinSymbolFilters(symbols),
	})
}

func (c *RtdsClient) SubscribeChainlink(symbols []string) error {
	return c.sendSubscriptions(ActionSubscribe, buildChainlinkSubscriptions(symbols))
}

func (c *RtdsClient) UnsubscribeBinance(symbols []string) error {
	return c.sendSubscription(ActionUnsubscribe, Subscription{
		Topic:   TopicCryptoPrices,
		Type:    SubscriptionTypeAll,
		Filters: joinSymbolFilters(symbols),
	})
}

func (c *RtdsClient) UnsubscribeChainlink(symbols []string) error {
	return c.sendSubscriptions(ActionUnsubscribe, buildChainlinkSubscriptions(symbols))
}

func buildChainlinkSubscriptions(symbols []string) []Subscription {
	if len(symbols) == 0 {
		return []Subscription{{Topic: TopicCryptoPricesChainlink, Type: SubscriptionTypeAll}}
	}
	subs := make([]Subscription, len(symbols))
	for i, sym := range symbols {
		subs[i] = Subscription{
			Topic:   TopicCryptoPricesChainlink,
			Type:    SubscriptionTypeAll,
			Filters: fmt.Sprintf(`{"symbol":"%s"}`, sym),
		}
	}
	return subs
}

func (c *RtdsClient) sendSubscription(action string, sub Subscription) error {
	return c.sendSubscriptions(action, []Subscription{sub})
}

func (c *RtdsClient) sendSubscriptions(action string, subs []Subscription) error {
	msg := SubscribeMessage{
		Action:        action,
		Subscriptions: subs,
	}

	return c.withConnWrite(func(conn *websocket.Conn) error {
		return conn.WriteJSON(msg)
	})
}

func (c *RtdsClient) handleMessages() {
	defer c.logger.Printf("RTDS message handler stopped")

	for {
		c.mu.RLock()
		conn := c.conn
		done := c.done
		c.mu.RUnlock()

		if conn == nil {
			return
		}

		select {
		case <-done:
			return
		default:
		}

		messageType, message, err := conn.ReadMessage()
		if err != nil {
			// Client-initiated disconnect: done channel already closed
			select {
			case <-done:
				return
			default:
			}
			if ce, ok := err.(*websocket.CloseError); ok {
				c.handleDisconnect(ce.Code, ce.Text)
			} else {
				c.handleDisconnect(-1, err.Error())
			}
			return
		}

		if messageType == websocket.TextMessage {
			if len(message) == 0 {
				continue
			}
			if bytes.Equal(message, msgPONG) || bytes.Equal(message, msgPong) {
				continue
			}
			if bytes.Equal(message, msgPing) || bytes.Equal(message, msgPING) {
				_ = c.withConnWrite(func(c *websocket.Conn) error {
					reply := msgPong
					if bytes.Equal(message, msgPING) {
						reply = msgPONG
					}
					return c.WriteMessage(websocket.TextMessage, reply)
				})
				continue
			}
			c.processMessage(message)
		}
	}
}

func (c *RtdsClient) processMessage(data []byte) {
	var msg RtdsMessage
	if err := json.Unmarshal(data, &msg); err != nil {
		c.handleError(fmt.Errorf("failed to parse RTDS message: %w", err))
		return
	}

	switch msg.Topic {
	case TopicCryptoPrices, TopicCryptoPricesChainlink:
		var payload CryptoPricePayload
		if err := json.Unmarshal(msg.Payload, &payload); err != nil {
			c.handleError(fmt.Errorf("failed to parse crypto price payload: %w", err))
			return
		}
		if c.callbacks.OnCryptoPrice != nil {
			c.callbacks.OnCryptoPrice(&payload)
		}
	default:
		c.logger.Printf("Unknown RTDS topic: %s", msg.Topic)
	}
}

func (c *RtdsClient) pingWorker() {
	c.mu.Lock()
	c.pingTicker = time.NewTicker(c.options.PingInterval)
	ticker := c.pingTicker
	c.mu.Unlock()

	defer ticker.Stop()

	for {
		c.mu.RLock()
		done := c.done
		c.mu.RUnlock()

		select {
		case <-done:
			return
		case <-ticker.C:
			err := c.withConnWrite(func(conn *websocket.Conn) error {
				return conn.WriteMessage(websocket.TextMessage, msgPING)
			})
			if err != nil {
				c.handleDisconnect(-1, "ping send failed: "+err.Error())
				return
			}
		}
	}
}

func (c *RtdsClient) handleError(err error) {
	if c.callbacks.OnError != nil {
		c.callbacks.OnError(err)
	} else {
		c.logger.Printf("Error: %v", err)
	}
}

func (c *RtdsClient) handleDisconnect(code int, reason string) {
	c.cleanup()
	c.forceCloseWithReason(code, reason)
	c.signalDone()

	if c.callbacks.OnDisconnect != nil {
		c.callbacks.OnDisconnect(code, reason)
	}

	c.mu.RLock()
	shouldReconnect := c.shouldReconnect
	autoReconnect := c.options.AutoReconnect
	c.mu.RUnlock()

	if shouldReconnect && autoReconnect {
		c.tryReconnect()
	}
}

func (c *RtdsClient) tryReconnect() {
	c.reconnectMu.Lock()
	defer c.reconnectMu.Unlock()

	c.mu.Lock()
	if c.reconnectTimer != nil {
		c.mu.Unlock()
		return
	}
	if c.options.MaxReconnectAttempts > 0 && c.reconnectAttempts >= c.options.MaxReconnectAttempts {
		c.mu.Unlock()
		return
	}

	c.reconnectAttempts++
	attempt := c.reconnectAttempts
	delay := c.options.ReconnectDelay
	if delay <= 0 {
		delay = 5 * time.Second
	}

	c.reconnectTimer = time.AfterFunc(delay, func() {
		c.mu.Lock()
		c.reconnectTimer = nil
		c.mu.Unlock()

		if err := c.Connect(); err != nil {
			c.handleDisconnect(-1, "reconnect failed: "+err.Error())
		}
	})
	c.mu.Unlock()

	if c.callbacks.OnReconnect != nil {
		c.callbacks.OnReconnect(attempt)
	}
}

func (c *RtdsClient) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.pingTicker != nil {
		c.pingTicker.Stop()
		c.pingTicker = nil
	}
	if c.reconnectTimer != nil {
		c.reconnectTimer.Stop()
		c.reconnectTimer = nil
	}
}

func (c *RtdsClient) forceCloseWithReason(code int, reason string) {
	c.mu.Lock()
	conn := c.conn
	c.conn = nil
	c.mu.Unlock()

	if conn == nil {
		return
	}
	_ = conn.WriteControl(
		websocket.CloseMessage,
		websocket.FormatCloseMessage(code, reason),
		time.Now().Add(2*time.Second),
	)
	_ = conn.Close()
}

func (c *RtdsClient) signalDone() {
	c.mu.RLock()
	done := c.done
	c.mu.RUnlock()
	if done == nil {
		return
	}
	c.closedOnce.Do(func() {
		close(done)
	})
}

func (c *RtdsClient) withConnWrite(fn func(conn *websocket.Conn) error) error {
	c.mu.RLock()
	conn := c.conn
	c.mu.RUnlock()
	if conn == nil {
		return fmt.Errorf("not connected")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return fn(conn)
}
