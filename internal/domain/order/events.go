package order

import "time"

type OrderPlacedEvent struct {
	OrderID       string    `json:"order_id"`
	ClientOrderID string    `json:"client_order_id"`
	AccountID     string    `json:"account_id"`
	Symbol        Symbol    `json:"symbol"`
	Side          OrderSide `json:"side"`
	Type          OrderType `json:"order_type"`
	Price         Price     `json:"price"`
	Quantity      Quantity  `json:"quantity"`
	Timestamp     time.Time `json:"timestamp"`
}

type OrderMatchedEvent struct {
	TradeID        string    `json:"trade_id"`
	Symbol         Symbol    `json:"symbol"`
	MakerOrderID   string    `json:"maker_order_id"`
	TakerOrderID   string    `json:"taker_order_id"`
	MakerAccountID string    `json:"maker_account_id"`
	TakerAccountID string    `json:"taker_account_id"`
	TakerSide      OrderSide `json:"taker_side"` // BUY or SELL
	Price          Price     `json:"price"`      // Matched execution price in VND
	Quantity       Quantity  `json:"quantity"`   // Executed lot quantity
	MakerRemaining Quantity  `json:"maker_remaining"`
	TakerRemaining Quantity  `json:"taker_remaining"`
	Timestamp      time.Time `json:"timestamp"`
}

type OrderCanceledEvent struct {
	OrderID          string    `json:"order_id"`
	AccountID        string    `json:"account_id"`
	Symbol           Symbol    `json:"symbol"`
	CanceledQuantity Quantity  `json:"canceled_quantity"`
	Reason           string    `json:"reason"`
	Timestamp        time.Time `json:"timestamp"`
}

type PriceLevelDepth struct {
	Price         Price    `json:"price"`
	TotalQuantity Quantity `json:"total_quantity"`
	OrderCount    uint32   `json:"order_count"`
}

type MarketDepthEvent struct {
	Symbol    Symbol            `json:"symbol"`
	Sequence  int64             `json:"sequence"`
	Bids      []PriceLevelDepth `json:"bids"` // Best bid first (descending)
	Asks      []PriceLevelDepth `json:"asks"` // Best ask first (ascending)
	Timestamp time.Time         `json:"timestamp"`
}
