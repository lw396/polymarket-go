package rtds

import (
	"log"
	"os"
	"sync"
	"testing"
	"time"
)

func TestConnectAndDisconnect(t *testing.T) {
	client := NewRtdsClient(&RtdsClientOptions{
		AutoReconnect: false,
		Logger:        log.New(os.Stdout, "[RTDS-TEST] ", log.LstdFlags),
		ProxyUrl:      "http://127.0.0.1:7890",
	})

	connected := make(chan struct{}, 1)
	disconnected := make(chan struct{}, 1)

	client.On(&RtdsCallbacks{
		OnConnect: func() {
			t.Log("Connected to RTDS")
			connected <- struct{}{}
		},
		OnDisconnect: func(code int, reason string) {
			t.Logf("Disconnected: code=%d reason=%s", code, reason)
			disconnected <- struct{}{}
		},
	})

	if err := client.Connect(); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}

	select {
	case <-connected:
		t.Log("Connect callback received")
	case <-time.After(10 * time.Second):
		t.Fatal("Timeout waiting for connection")
	}

	if !client.IsConnected() {
		t.Fatal("Client should be connected")
	}

	client.Disconnect()

	select {
	case <-disconnected:
		t.Log("Disconnect callback received")
	case <-time.After(5 * time.Second):
		t.Fatal("Timeout waiting for disconnection")
	}
}

func TestSubscribeBinanceAll(t *testing.T) {
	client := NewRtdsClient(&RtdsClientOptions{
		AutoReconnect: false,
		Logger:        log.New(os.Stdout, "[RTDS-TEST] ", log.LstdFlags),
	})

	var mu sync.Mutex
	prices := make(map[string]float64)

	client.On(&RtdsCallbacks{
		OnCryptoPrice: func(payload *CryptoPricePayload) {
			mu.Lock()
			prices[payload.Symbol] = payload.Value
			mu.Unlock()
			t.Logf("Price update: %s = %.2f", payload.Symbol, payload.Value)
		},
		OnError: func(err error) {
			t.Logf("Error: %v", err)
		},
	})

	if err := client.Connect(); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect()

	if err := client.SubscribeBinance(nil); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	// Wait for price updates
	time.Sleep(15 * time.Second)

	mu.Lock()
	count := len(prices)
	mu.Unlock()

	if count == 0 {
		t.Fatal("Expected to receive at least one price update from Binance")
	}
	t.Logf("Received prices for %d symbols", count)
}

func TestSubscribeBinanceSpecific(t *testing.T) {
	client := NewRtdsClient(&RtdsClientOptions{
		AutoReconnect: false,
		Logger:        log.New(os.Stdout, "[RTDS-TEST] ", log.LstdFlags),
	})

	received := make(chan *CryptoPricePayload, 10)

	client.On(&RtdsCallbacks{
		OnCryptoPrice: func(payload *CryptoPricePayload) {
			if payload.Symbol == SymbolBtcUsdt {
				received <- payload
			}
		},
		OnError: func(err error) {
			t.Logf("Error: %v", err)
		},
	})

	if err := client.Connect(); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect()

	if err := client.SubscribeBinance([]string{SymbolBtcUsdt}); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	select {
	case p := <-received:
		t.Logf("BTC price: %.2f at %d", p.Value, p.Timestamp)
		if p.Value <= 0 {
			t.Fatal("BTC price should be positive")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("Timeout waiting for BTC price update")
	}
}

func TestSubscribeChainlinkAll(t *testing.T) {
	client := NewRtdsClient(&RtdsClientOptions{
		AutoReconnect: false,
		Logger:        log.New(os.Stdout, "[RTDS-TEST] ", log.LstdFlags),
	})

	var mu sync.Mutex
	prices := make(map[string]float64)

	client.On(&RtdsCallbacks{
		OnCryptoPrice: func(payload *CryptoPricePayload) {
			mu.Lock()
			prices[payload.Symbol] = payload.Value
			mu.Unlock()
			t.Logf("Chainlink price: %s = %.2f", payload.Symbol, payload.Value)
		},
		OnError: func(err error) {
			t.Logf("Error: %v", err)
		},
	})

	if err := client.Connect(); err != nil {
		t.Fatalf("Failed to connect: %v", err)
	}
	defer client.Disconnect()

	if err := client.SubscribeChainlink(nil); err != nil {
		t.Fatalf("Failed to subscribe: %v", err)
	}

	time.Sleep(15 * time.Second)

	mu.Lock()
	count := len(prices)
	mu.Unlock()

	if count == 0 {
		t.Fatal("Expected to receive at least one price update from Chainlink")
	}
	t.Logf("Received Chainlink prices for %d symbols", count)
}
