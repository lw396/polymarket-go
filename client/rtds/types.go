package rtds

import "encoding/json"

// Subscription topics
const (
	TopicCryptoPrices          = "crypto_prices"
	TopicCryptoPricesChainlink = "crypto_prices_chainlink"
)

// Subscribe/unsubscribe actions
const (
	ActionSubscribe   = "subscribe"
	ActionUnsubscribe = "unsubscribe"
)

// Subscription type values
const (
	SubscriptionTypeUpdate = "update"
	SubscriptionTypeAll    = "*"
)

// WebSocket text control messages
var (
	msgPing = []byte("ping")
	msgPING = []byte("PING")
	msgPong = []byte("pong")
	msgPONG = []byte("PONG")
)

// Binance trading pair symbols (lowercase concatenated format)
const (
	SymbolBtcUsdt = "btcusdt"
	SymbolEthUsdt = "ethusdt"
	SymbolSolUsdt = "solusdt"
	SymbolXrpUsdt = "xrpusdt"
)

// Chainlink trading pair symbols (slash-separated format)
const (
	SymbolBtcUsd = "btc/usd"
	SymbolEthUsd = "eth/usd"
	SymbolSolUsd = "sol/usd"
	SymbolXrpUsd = "xrp/usd"
)

// Subscription is a single subscription entry in a subscribe/unsubscribe message.
type Subscription struct {
	Topic   string `json:"topic"`
	Type    string `json:"type"`
	Filters string `json:"filters,omitempty"`
}

// SubscribeMessage is the top-level message sent to subscribe or unsubscribe.
type SubscribeMessage struct {
	Action        string         `json:"action"`
	Subscriptions []Subscription `json:"subscriptions"`
}

// RtdsMessage is the envelope for all incoming RTDS messages.
type RtdsMessage struct {
	Topic     string          `json:"topic"`
	Type      string          `json:"type"`
	Timestamp int64           `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
}

// CryptoPricePayload represents a price update for a trading pair.
// Shared by both Binance and Chainlink sources.
type CryptoPricePayload struct {
	Symbol    string  `json:"symbol"`
	Timestamp int64   `json:"timestamp"`
	Value     float64 `json:"value"`
}
