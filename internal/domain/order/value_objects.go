package order

import "fmt"

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

// Price represents currency ticks in VND (64-bit integer to prevent floating-point drift)
type Price int64

func (p Price) Int64() int64 {
	return int64(p)
}

func (p Price) String() string {
	return fmt.Sprintf("%d VND", int64(p))
}

// Quantity represents stock lot units (unsigned 32-bit integer)
type Quantity uint32

func (q Quantity) Uint32() uint32 {
	return uint32(q)
}

type Symbol string

func (s Symbol) String() string {
	return string(s)
}
