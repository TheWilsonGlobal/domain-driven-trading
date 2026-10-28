package order

import "github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"

type OrderSide string

const (
	SideBuy  OrderSide = "BUY"
	SideSell OrderSide = "SELL"
)

func (s OrderSide) IsValid() bool {
	return s == SideBuy || s == SideSell
}

type OrderType string

const (
	TypeLimit  OrderType = "LIMIT"
	TypeMarket OrderType = "MARKET"
)

type OrderStatus string

const (
	StatusNew             OrderStatus = "NEW"
	StatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	StatusFilled          OrderStatus = "FILLED"
	StatusCanceled        OrderStatus = "CANCELED"
	StatusRejected        OrderStatus = "REJECTED"
)

// Price, Quantity and Symbol live in the shared domain/common package so that
// order and account do not import each other. The aliases keep this package's
// API unchanged.
type (
	Price    = common.Price
	Quantity = common.Quantity
	Symbol   = common.Symbol
)

const (
	TickSize = common.TickSize
	MaxPrice = common.MaxPrice
	MaxQty   = common.MaxQty
)
