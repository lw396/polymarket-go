package types

import (
	"encoding/json"
	"fmt"
)

type EventType string

const (
	EventTypeBook           EventType = "book"
	EventTypePriceChange    EventType = "price_change"
	EventTypeTickSizeChange EventType = "tick_size_change"
	EventTypeLastTradePrice EventType = "last_trade_price"
)

type BookMessage struct {
	EventType EventType      `json:"event_type"`
	AssetID   string         `json:"asset_id"`
	Market    string         `json:"market"`
	Timestamp string         `json:"timestamp"`
	Hash      string         `json:"hash"`
	Bids      []OrderSummary `json:"bids"`
	Asks      []OrderSummary `json:"asks"`
}

func (m *BookMessage) Validate() error {
	if m.EventType != EventTypeBook {
		return fmt.Errorf("invalid event_type: expected 'book', got '%s'", m.EventType)
	}
	if m.AssetID == "" {
		return fmt.Errorf("asset_id is required")
	}
	if m.Market == "" {
		return fmt.Errorf("market is required")
	}
	if m.Timestamp == "" {
		return fmt.Errorf("timestamp is required")
	}
	if m.Hash == "" {
		return fmt.Errorf("hash is required")
	}
	return nil
}

type PriceChange struct {
	AssetID string `json:"asset_id"`
	Price   string `json:"price"`
	Size    string `json:"size"`
	Side    Side   `json:"side"`
	Hash    string `json:"hash"`
	BestBid string `json:"best_bid"`
	BestAsk string `json:"best_ask"`
}

func (pc *PriceChange) Validate() error {
	if pc.AssetID == "" {
		return fmt.Errorf("asset_id is required")
	}
	if pc.Price == "" {
		return fmt.Errorf("price is required")
	}
	if pc.Size == "" {
		return fmt.Errorf("size is required")
	}
	if pc.Side != SideBuy && pc.Side != SideSell {
		return fmt.Errorf("invalid side: must be 'BUY' or 'SELL', got '%s'", pc.Side)
	}
	if pc.Hash == "" {
		return fmt.Errorf("hash is required")
	}
	return nil
}

type PriceChangeMessage struct {
	EventType    EventType     `json:"event_type"`
	Market       string        `json:"market"`
	PriceChanges []PriceChange `json:"price_changes"`
	Timestamp    string        `json:"timestamp"`
}

func (m *PriceChangeMessage) Validate() error {
	if m.EventType != EventTypePriceChange {
		return fmt.Errorf("invalid event_type: expected 'price_change', got '%s'", m.EventType)
	}
	if m.Market == "" {
		return fmt.Errorf("market is required")
	}
	if m.Timestamp == "" {
		return fmt.Errorf("timestamp is required")
	}
	if len(m.PriceChanges) == 0 {
		return fmt.Errorf("price_changes array cannot be empty")
	}
	for i, pc := range m.PriceChanges {
		if err := pc.Validate(); err != nil {
			return fmt.Errorf("price_changes[%d]: %w", i, err)
		}
	}
	return nil
}

type TickSizeChangeMessage struct {
	EventType   EventType `json:"event_type"`
	AssetID     string    `json:"asset_id"`
	Market      string    `json:"market"`
	OldTickSize string    `json:"old_tick_size"`
	NewTickSize string    `json:"new_tick_size"`
	Timestamp   string    `json:"timestamp"`
}

func (m *TickSizeChangeMessage) Validate() error {
	if m.EventType != EventTypeTickSizeChange {
		return fmt.Errorf("invalid event_type: expected 'tick_size_change', got '%s'", m.EventType)
	}
	if m.AssetID == "" {
		return fmt.Errorf("asset_id is required")
	}
	if m.Market == "" {
		return fmt.Errorf("market is required")
	}
	if m.OldTickSize == "" {
		return fmt.Errorf("old_tick_size is required")
	}
	if m.NewTickSize == "" {
		return fmt.Errorf("new_tick_size is required")
	}
	if m.Timestamp == "" {
		return fmt.Errorf("timestamp is required")
	}
	return nil
}

type LastTradePriceMessage struct {
	EventType  EventType `json:"event_type"`
	AssetID    string    `json:"asset_id"`
	Market     string    `json:"market"`
	Price      string    `json:"price"`
	Side       Side      `json:"side"`
	Size       string    `json:"size"`
	FeeRateBps string    `json:"fee_rate_bps"`
	Timestamp  string    `json:"timestamp"`
}

func (m *LastTradePriceMessage) Validate() error {
	if m.EventType != EventTypeLastTradePrice {
		return fmt.Errorf("invalid event_type: expected 'last_trade_price', got '%s'", m.EventType)
	}
	if m.AssetID == "" {
		return fmt.Errorf("asset_id is required")
	}
	if m.Market == "" {
		return fmt.Errorf("market is required")
	}
	if m.Price == "" {
		return fmt.Errorf("price is required")
	}
	if m.Side != SideBuy && m.Side != SideSell {
		return fmt.Errorf("invalid side: must be 'BUY' or 'SELL', got '%s'", m.Side)
	}
	if m.Size == "" {
		return fmt.Errorf("size is required")
	}
	if m.Timestamp == "" {
		return fmt.Errorf("timestamp is required")
	}
	return nil
}

type MarketChannelMessage interface {
	Validate() error
	GetEventType() EventType
}

func (m *BookMessage) GetEventType() EventType {
	return m.EventType
}

func (m *PriceChangeMessage) GetEventType() EventType {
	return m.EventType
}

func (m *TickSizeChangeMessage) GetEventType() EventType {
	return m.EventType
}

func (m *LastTradePriceMessage) GetEventType() EventType {
	return m.EventType
}

func ParseMarketChannelMessage(data []byte) (MarketChannelMessage, error) {
	var eventTypeWrapper struct {
		EventType EventType `json:"event_type"`
	}

	if err := json.Unmarshal(data, &eventTypeWrapper); err != nil {
		return nil, fmt.Errorf("failed to parse event_type: %w", err)
	}

	switch eventTypeWrapper.EventType {
	case EventTypeBook:
		var msg BookMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse book message: %w", err)
		}
		if err := msg.Validate(); err != nil {
			return nil, fmt.Errorf("invalid book message: %w", err)
		}
		return &msg, nil

	case EventTypePriceChange:
		var msg PriceChangeMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse price_change message: %w", err)
		}
		if err := msg.Validate(); err != nil {
			return nil, fmt.Errorf("invalid price_change message: %w", err)
		}
		return &msg, nil

	case EventTypeTickSizeChange:
		var msg TickSizeChangeMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse tick_size_change message: %w", err)
		}
		if err := msg.Validate(); err != nil {
			return nil, fmt.Errorf("invalid tick_size_change message: %w", err)
		}
		return &msg, nil

	case EventTypeLastTradePrice:
		var msg LastTradePriceMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse last_trade_price message: %w", err)
		}
		if err := msg.Validate(); err != nil {
			return nil, fmt.Errorf("invalid last_trade_price message: %w", err)
		}
		return &msg, nil

	default:
		return nil, fmt.Errorf("unknown event_type: %s", eventTypeWrapper.EventType)
	}
}

// AsBookMessage attempts to cast to BookMessage
func AsBookMessage(msg MarketChannelMessage) (*BookMessage, bool) {
	if m, ok := msg.(*BookMessage); ok {
		return m, true
	}
	return nil, false
}

// AsPriceChangeMessage attempts to cast to PriceChangeMessage
func AsPriceChangeMessage(msg MarketChannelMessage) (*PriceChangeMessage, bool) {
	if m, ok := msg.(*PriceChangeMessage); ok {
		return m, true
	}
	return nil, false
}

// AsTickSizeChangeMessage attempts to cast to TickSizeChangeMessage
func AsTickSizeChangeMessage(msg MarketChannelMessage) (*TickSizeChangeMessage, bool) {
	if m, ok := msg.(*TickSizeChangeMessage); ok {
		return m, true
	}
	return nil, false
}

// AsLastTradePriceMessage attempts to cast to LastTradePriceMessage
func AsLastTradePriceMessage(msg MarketChannelMessage) (*LastTradePriceMessage, bool) {
	if m, ok := msg.(*LastTradePriceMessage); ok {
		return m, true
	}
	return nil, false
}

// User Channel event types
const (
	EventTypeTrade EventType = "trade"
	EventTypeOrder EventType = "order"
)

// Trade status values
const (
	TradeStatusMatched   = "MATCHED"
	TradeStatusMined     = "MINED"
	TradeStatusConfirmed = "CONFIRMED"
	TradeStatusRetrying  = "RETRYING"
	TradeStatusFailed    = "FAILED"
)

// Order event type values
const (
	UserOrderPlacement    = "PLACEMENT"
	UserOrderUpdate       = "UPDATE"
	UserOrderCancellation = "CANCELLATION"
)

// UserChannelMessage is the interface for all user channel messages.
type UserChannelMessage interface {
	Validate() error
	GetEventType() EventType
}

// MakerOrderDetail represents a maker order in a trade event.
type MakerOrderDetail struct {
	AssetID       string `json:"asset_id"`
	MatchedAmount string `json:"matched_amount"`
	OrderID       string `json:"order_id"`
	Outcome       string `json:"outcome"`
	Owner         string `json:"owner"`
	Price         string `json:"price"`
}

// TradeEvent represents a trade event from the user channel.
type TradeEvent struct {
	EventType    EventType          `json:"event_type"`
	AssetID      string             `json:"asset_id"`
	ID           string             `json:"id"`
	LastUpdate   string             `json:"last_update"`
	MakerOrders  []MakerOrderDetail `json:"maker_orders"`
	Market       string             `json:"market"`
	MatchTime    string             `json:"matchtime"`
	Outcome      string             `json:"outcome"`
	Owner        string             `json:"owner"`
	Price        string             `json:"price"`
	Side         Side               `json:"side"`
	Size         string             `json:"size"`
	Status       string             `json:"status"`
	TakerOrderID string             `json:"taker_order_id"`
	Timestamp    string             `json:"timestamp"`
	TradeOwner   string             `json:"trade_owner"`
	Type         string             `json:"type"`
}

func (m *TradeEvent) Validate() error {
	if m.EventType != EventTypeTrade {
		return fmt.Errorf("invalid event_type: expected 'trade', got '%s'", m.EventType)
	}
	if m.ID == "" {
		return fmt.Errorf("id is required")
	}
	if m.AssetID == "" {
		return fmt.Errorf("asset_id is required")
	}
	if m.Market == "" {
		return fmt.Errorf("market is required")
	}
	return nil
}

func (m *TradeEvent) GetEventType() EventType {
	return m.EventType
}

// OrderEvent represents an order event from the user channel.
type OrderEvent struct {
	EventType       EventType `json:"event_type"`
	AssetID         string    `json:"asset_id"`
	AssociateTrades []string  `json:"associate_trades"`
	ID              string    `json:"id"`
	Market          string    `json:"market"`
	OrderOwner      string    `json:"order_owner"`
	OriginalSize    string    `json:"original_size"`
	Outcome         string    `json:"outcome"`
	Owner           string    `json:"owner"`
	Price           string    `json:"price"`
	Side            Side      `json:"side"`
	SizeMatched     string    `json:"size_matched"`
	Timestamp       string    `json:"timestamp"`
	Type            string    `json:"type"`
}

func (m *OrderEvent) Validate() error {
	if m.EventType != EventTypeOrder {
		return fmt.Errorf("invalid event_type: expected 'order', got '%s'", m.EventType)
	}
	if m.ID == "" {
		return fmt.Errorf("id is required")
	}
	if m.AssetID == "" {
		return fmt.Errorf("asset_id is required")
	}
	if m.Market == "" {
		return fmt.Errorf("market is required")
	}
	return nil
}

func (m *OrderEvent) GetEventType() EventType {
	return m.EventType
}

// ParseUserChannelMessage parses a JSON message from the user channel.
func ParseUserChannelMessage(data []byte) (UserChannelMessage, error) {
	var eventTypeWrapper struct {
		EventType EventType `json:"event_type"`
	}

	if err := json.Unmarshal(data, &eventTypeWrapper); err != nil {
		return nil, fmt.Errorf("failed to parse event_type: %w", err)
	}

	switch eventTypeWrapper.EventType {
	case EventTypeTrade:
		var msg TradeEvent
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse trade message: %w", err)
		}
		if err := msg.Validate(); err != nil {
			return nil, fmt.Errorf("invalid trade message: %w", err)
		}
		return &msg, nil
	case EventTypeOrder:
		var msg OrderEvent
		if err := json.Unmarshal(data, &msg); err != nil {
			return nil, fmt.Errorf("failed to parse order message: %w", err)
		}
		if err := msg.Validate(); err != nil {
			return nil, fmt.Errorf("invalid order message: %w", err)
		}
		return &msg, nil
	default:
		return nil, fmt.Errorf("unknown user channel event_type: %s", eventTypeWrapper.EventType)
	}
}

// AsTradeEvent attempts to cast to TradeEvent
func AsTradeEvent(msg UserChannelMessage) (*TradeEvent, bool) {
	if m, ok := msg.(*TradeEvent); ok {
		return m, true
	}
	return nil, false
}

// AsOrderEvent attempts to cast to OrderEvent
func AsOrderEvent(msg UserChannelMessage) (*OrderEvent, bool) {
	if m, ok := msg.(*OrderEvent); ok {
		return m, true
	}
	return nil, false
}
