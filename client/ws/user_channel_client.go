package ws

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/gorilla/websocket"
	"github.com/lw396/polymarket-go/client/clob"
	"github.com/lw396/polymarket-go/client/clob/clob_types"
	"github.com/lw396/polymarket-go/client/config"
	"github.com/lw396/polymarket-go/client/endpoint"
	"github.com/lw396/polymarket-go/client/types"
)

type UserChannelOptions struct {
	Markets []string

	ApiKeyCreds *types.ApiKeyCreds

	AutoReconnect bool

	ReconnectDelay time.Duration

	MaxReconnectAttempts int

	Debug bool

	Logger *log.Logger

	ProxyUrl string
}

type UserChannelCallbacks struct {
	OnTradeEvent func(msg *types.TradeEvent)
	OnOrderEvent func(msg *types.OrderEvent)
	OnMessage    func(msg types.UserChannelMessage)
	OnError      func(error)
	OnConnect    func()
	OnDisconnect func(code int, reason string)
	OnReconnect  func(attempt int)
}

type UserChannelClient struct {
	mu                sync.RWMutex
	writeMu           sync.Mutex
	reconnectMu       sync.Mutex
	closedOnce        sync.Once
	clobClient        *clob.ClobClient
	options           *UserChannelOptions
	callbacks         *UserChannelCallbacks
	conn              *websocket.Conn
	pingTicker        *time.Ticker
	reconnectTimer    *time.Timer
	done              chan struct{}
	reconnectAttempts int
	isConnecting      bool
	shouldReconnect   bool
	logger            *log.Logger
	cachedApiKey      *types.ApiKeyCreds
}

func NewUserChannelClient(clobClient *clob.ClobClient, options *UserChannelOptions) *UserChannelClient {
	if options == nil {
		options = &UserChannelOptions{}
	}

	if options.AutoReconnect && options.ReconnectDelay == 0 {
		options.ReconnectDelay = 5 * time.Second
	}

	logger := options.Logger
	if logger == nil {
		logger = log.Default()
	}

	return &UserChannelClient{
		clobClient:      clobClient,
		options:         options,
		callbacks:       &UserChannelCallbacks{},
		done:            nil,
		shouldReconnect: true,
		logger:          logger,
		cachedApiKey:    options.ApiKeyCreds,
	}
}

func (uc *UserChannelClient) On(callbacks *UserChannelCallbacks) *UserChannelClient {
	uc.callbacks = callbacks
	return uc
}

func (uc *UserChannelClient) Connect() error {
	uc.mu.Lock()
	if uc.isConnecting || (uc.conn != nil && uc.IsConnected()) {
		uc.mu.Unlock()
		return nil
	}
	uc.isConnecting = true
	uc.shouldReconnect = true
	uc.mu.Unlock()

	apiKey := uc.getOrDeriveApiKey()
	if apiKey == nil {
		uc.mu.Lock()
		uc.isConnecting = false
		uc.mu.Unlock()
		return fmt.Errorf("failed to obtain API key credentials")
	}

	fullURL := endpoint.WsUserUrl
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"http/1.1"},
	}
	dialer := websocket.Dialer{TLSClientConfig: tlsConfig}
	if uc.options.ProxyUrl != "" {
		proxyUrl, err := url.Parse(uc.options.ProxyUrl)
		if err != nil {
			uc.mu.Lock()
			uc.isConnecting = false
			uc.mu.Unlock()
			return fmt.Errorf("failed to parse proxy url: %w", err)
		}
		dialer.Proxy = http.ProxyURL(proxyUrl)
	}

	conn, _, err := dialer.Dial(fullURL, nil)
	if err != nil {
		uc.mu.Lock()
		uc.isConnecting = false
		uc.mu.Unlock()
		return fmt.Errorf("failed to connect to user channel WebSocket: %w", err)
	}

	uc.mu.Lock()
	uc.conn = conn
	uc.isConnecting = false
	uc.reconnectAttempts = 0
	uc.done = make(chan struct{})
	uc.closedOnce = sync.Once{}
	uc.mu.Unlock()

	conn.SetPongHandler(func(appData string) error {
		uc.logger.Printf("Received CONTROL PONG: %s", appData)
		return nil
	})
	conn.SetPingHandler(func(appData string) error {
		_ = uc.withConnWrite(func(c *websocket.Conn) error {
			return c.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(5*time.Second))
		})
		return nil
	})

	uc.logger.Printf("User channel WebSocket connected")

	if err = uc.sendAuthSubscription(apiKey); err != nil {
		uc.forceCloseWithReason(-1, fmt.Sprintf("auth subscription failed: %v", err))
		uc.handleDisconnect(-1, fmt.Sprintf("auth subscription failed: %v", err))
		return fmt.Errorf("failed to send authenticated subscription: %w", err)
	}

	go uc.handleMessages()
	go uc.pingWorker()

	if uc.callbacks.OnConnect != nil {
		uc.callbacks.OnConnect()
	}
	return nil
}

func (uc *UserChannelClient) Disconnect() {
	uc.mu.Lock()
	uc.shouldReconnect = false
	uc.mu.Unlock()

	uc.cleanup()

	uc.forceCloseWithReason(websocket.CloseNormalClosure, "client disconnect")
	uc.signalDone()
}

func (uc *UserChannelClient) Subscribe(markets []string) error {
	uc.mu.Lock()
	existing := make(map[string]bool, len(uc.options.Markets))
	for _, m := range uc.options.Markets {
		existing[m] = true
	}
	for _, m := range markets {
		if !existing[m] {
			uc.options.Markets = append(uc.options.Markets, m)
			existing[m] = true
		}
	}
	uc.mu.Unlock()

	if uc.IsConnected() {
		apiKey := uc.getOrDeriveApiKey()
		if apiKey == nil {
			return fmt.Errorf("no API credentials available")
		}
		return uc.sendAuthSubscription(apiKey)
	}

	return nil
}

func (uc *UserChannelClient) Unsubscribe(markets []string) {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	filtered := make([]string, 0, len(uc.options.Markets))
	for _, m := range uc.options.Markets {
		keep := true
		for _, unsub := range markets {
			if m == unsub {
				keep = false
				break
			}
		}
		if keep {
			filtered = append(filtered, m)
		}
	}
	uc.options.Markets = filtered
}

func (uc *UserChannelClient) IsConnected() bool {
	uc.mu.RLock()
	defer uc.mu.RUnlock()
	return uc.conn != nil
}

func (uc *UserChannelClient) Wait() {
	<-uc.done
}

func (uc *UserChannelClient) getOrDeriveApiKey() *types.ApiKeyCreds {
	uc.mu.RLock()
	cached := uc.cachedApiKey
	uc.mu.RUnlock()
	if cached != nil {
		return cached
	}

	option := clob_types.ClobOption{
		TurnkeyAccount: common.Address{},
		SafeAccount:    common.Address{},
	}
	apiKey, err := uc.clobClient.DeriveApiKey(nil, option)
	if err != nil {
		uc.logger.Printf("Failed to derive API key: %v", err)
		return nil
	}

	uc.mu.Lock()
	uc.cachedApiKey = apiKey
	uc.mu.Unlock()

	return apiKey
}

func (uc *UserChannelClient) sendAuthSubscription(apiKey *types.ApiKeyCreds) error {
	uc.mu.RLock()
	conn := uc.conn
	markets := uc.options.Markets
	uc.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("not connected")
	}
	if len(markets) == 0 {
		return nil
	}

	message := map[string]interface{}{
		"auth": map[string]string{
			"apiKey":     apiKey.Key,
			"secret":     apiKey.Secret,
			"passphrase": apiKey.Passphrase,
		},
		"markets": markets,
		"type":    "user",
	}

	return uc.withConnWrite(func(conn *websocket.Conn) error {
		return conn.WriteJSON(message)
	})
}

func (uc *UserChannelClient) handleMessages() {
	defer uc.logger.Printf("User channel message handler stopped")

	for {
		uc.mu.RLock()
		conn := uc.conn
		done := uc.done
		uc.mu.RUnlock()

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
			select {
			case <-done:
				return
			default:
			}
			if ce, ok := err.(*websocket.CloseError); ok {
				uc.logger.Printf("ReadMessage CloseError: code=%v, reason=%v", ce.Code, ce.Text)
				uc.handleDisconnect(ce.Code, ce.Text)
			} else {
				uc.logger.Printf("ReadMessage error: %v", err)
				uc.handleDisconnect(-1, err.Error())
			}
			return
		}

		if messageType == websocket.TextMessage {
			txt := string(message)

			if txt == "PONG" || txt == "pong" {
				continue
			}
			if txt == "ping" || txt == "PING" {
				_ = uc.withConnWrite(func(c *websocket.Conn) error {
					reply := "pong"
					if txt == "PING" {
						reply = "PONG"
					}
					return c.WriteMessage(websocket.TextMessage, []byte(reply))
				})
				continue
			}
			uc.processMessage(message)
		}
	}
}

func (uc *UserChannelClient) processMessage(data []byte) {
	var messages []json.RawMessage
	if err := json.Unmarshal(data, &messages); err == nil {
		for _, msgData := range messages {
			uc.parseAndDispatch(msgData)
		}
	} else {
		uc.parseAndDispatch(data)
	}
}

func (uc *UserChannelClient) parseAndDispatch(data []byte) {
	msg, err := types.ParseUserChannelMessage(data)
	if err != nil {
		uc.handleError(fmt.Errorf("failed to parse user channel message: %w", err))
		uc.logger.Printf("Raw message: %s", string(data))
		return
	}

	switch msg.GetEventType() {
	case types.EventTypeTrade:
		if tradeMsg, ok := types.AsTradeEvent(msg); ok && uc.callbacks.OnTradeEvent != nil {
			uc.callbacks.OnTradeEvent(tradeMsg)
		}
	case types.EventTypeOrder:
		if orderMsg, ok := types.AsOrderEvent(msg); ok && uc.callbacks.OnOrderEvent != nil {
			uc.callbacks.OnOrderEvent(orderMsg)
		}
	}

	if uc.callbacks.OnMessage != nil {
		uc.callbacks.OnMessage(msg)
	}
}

func (uc *UserChannelClient) pingWorker() {
	uc.mu.Lock()
	uc.pingTicker = time.NewTicker(config.GetWsPingInterval())
	ticker := uc.pingTicker
	uc.mu.Unlock()

	defer ticker.Stop()

	for {
		uc.mu.RLock()
		done := uc.done
		uc.mu.RUnlock()

		select {
		case <-done:
			return
		case <-ticker.C:
			err := uc.withConnWrite(func(conn *websocket.Conn) error {
				return conn.WriteMessage(websocket.TextMessage, []byte("PING"))
			})
			if err != nil {
				uc.logger.Printf("failed to send ping: %v", err)
				uc.handleDisconnect(-1, "ping send failed: "+err.Error())
				return
			}
		}
	}
}

func (uc *UserChannelClient) handleError(err error) {
	if uc.callbacks.OnError != nil {
		uc.callbacks.OnError(err)
	} else {
		uc.logger.Printf("Error: %v", err)
	}
}

func (uc *UserChannelClient) handleDisconnect(code int, reason string) {
	uc.cleanup()
	uc.forceCloseWithReason(code, reason)
	uc.signalDone()

	if uc.callbacks.OnDisconnect != nil {
		uc.callbacks.OnDisconnect(code, reason)
	}

	uc.mu.RLock()
	shouldReconnect := uc.shouldReconnect
	autoReconnect := uc.options.AutoReconnect
	uc.mu.RUnlock()

	if shouldReconnect && autoReconnect {
		uc.tryReconnect()
	}
}

func (uc *UserChannelClient) tryReconnect() {
	uc.reconnectMu.Lock()
	defer uc.reconnectMu.Unlock()
	uc.mu.RLock()
	hasTimer := uc.reconnectTimer != nil
	uc.mu.RUnlock()
	if hasTimer {
		return
	}

	uc.mu.Lock()
	if uc.options.MaxReconnectAttempts > 0 && uc.reconnectAttempts >= uc.options.MaxReconnectAttempts {
		uc.mu.Unlock()
		uc.logger.Println("Max reconnect attempts reached")
		return
	}

	uc.reconnectAttempts++
	attempt := uc.reconnectAttempts
	delay := uc.options.ReconnectDelay
	if delay <= 0 {
		delay = 5 * time.Second
	}
	uc.mu.Unlock()

	if uc.callbacks.OnReconnect != nil {
		uc.callbacks.OnReconnect(attempt)
	}

	uc.mu.Lock()
	uc.reconnectTimer = time.AfterFunc(delay, func() {
		uc.mu.Lock()
		uc.reconnectTimer = nil
		uc.mu.Unlock()

		if err := uc.Connect(); err != nil {
			uc.logger.Printf("Reconnect failed: %v", err)
			uc.handleDisconnect(-1, "reconnect failed: "+err.Error())
		}
	})
	uc.mu.Unlock()
}

func (uc *UserChannelClient) signalDone() {
	uc.mu.RLock()
	done := uc.done
	uc.mu.RUnlock()
	if done == nil {
		return
	}
	uc.closedOnce.Do(func() {
		close(done)
	})
}

func (uc *UserChannelClient) forceCloseWithReason(code int, reason string) {
	uc.mu.Lock()
	conn := uc.conn
	uc.conn = nil
	uc.mu.Unlock()

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

func (uc *UserChannelClient) cleanup() {
	uc.mu.Lock()
	defer uc.mu.Unlock()

	if uc.pingTicker != nil {
		uc.pingTicker.Stop()
		uc.pingTicker = nil
	}

	if uc.reconnectTimer != nil {
		uc.reconnectTimer.Stop()
		uc.reconnectTimer = nil
	}
}

func (uc *UserChannelClient) withConnWrite(fn func(conn *websocket.Conn) error) error {
	uc.mu.RLock()
	conn := uc.conn
	uc.mu.RUnlock()
	if conn == nil {
		return fmt.Errorf("not connected")
	}

	uc.writeMu.Lock()
	defer uc.writeMu.Unlock()
	return fn(conn)
}
