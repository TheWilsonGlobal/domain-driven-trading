package order

import (
	"fmt"
	"math/bits"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
)

// Order represents an immutable/mutable active order in memory.
// It includes doubly-linked pointers Prev and Next to support O(1) queue deletion.
type Order struct {
	ID             string      `json:"order_id"`
	ClientOrderID  string      `json:"client_order_id"`
	AccountID      string      `json:"account_id"`
	Symbol         Symbol      `json:"symbol"`
	Side           OrderSide   `json:"side"`
	Type           OrderType   `json:"order_type"`
	Price          Price       `json:"price"` // VND
	Quantity       Quantity    `json:"quantity"`
	FilledQuantity Quantity    `json:"filled_quantity"`
	Status         OrderStatus `json:"status"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`

	// Cash reservation of a BUY order (VND). ReservedAmount is what was locked
	// on the account at placement; RemainingReserved is the part still locked.
	// Each fill consumes a proportional share; the final fill consumes all of
	// it. A terminated order (cancel, market remainder, self-trade stop) keeps
	// its RemainingReserved so the caller can hand exactly that back.
	// A SELL order reserves shares: that is always RemainingQuantity().
	ReservedAmount    int64 `json:"-"`
	RemainingReserved int64 `json:"-"`

	// Pointers for O(1) doubly-linked FIFO order queue at a single price level
	Prev *Order `json:"-"`
	Next *Order `json:"-"`
}

func NewOrder(id, clientOrderID, accountID string, symbol Symbol, side OrderSide, orderType OrderType, price Price, qty Quantity) (*Order, error) {
	if !side.IsValid() {
		return nil, common.ErrInvalidSide
	}
	if orderType != TypeLimit && orderType != TypeMarket {
		return nil, common.ErrInvalidOrderType
	}
	if qty == 0 {
		return nil, common.ErrInvalidQuantity
	}
	if orderType == TypeLimit && price <= 0 {
		return nil, common.ErrInvalidPrice
	}

	now := time.Now()
	return &Order{
		ID:             id,
		ClientOrderID:  clientOrderID,
		AccountID:      accountID,
		Symbol:         symbol,
		Side:           side,
		Type:           orderType,
		Price:          price,
		Quantity:       qty,
		FilledQuantity: 0,
		Status:         StatusNew,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

func (o *Order) RemainingQuantity() Quantity {
	if o.FilledQuantity >= o.Quantity {
		return 0
	}
	return o.Quantity - o.FilledQuantity
}

// Reserve records the cash locked for a BUY order.
func (o *Order) Reserve(amount int64) {
	o.ReservedAmount = amount
	o.RemainingReserved = amount
}

// LockedShares is the number of shares a SELL order still holds locked.
func (o *Order) LockedShares() Quantity {
	if o.Side != SideSell {
		return 0
	}
	return o.RemainingQuantity()
}

// consumeReservation takes the share of the remaining reservation that belongs
// to filling qty of the unfilled quantity. The last unit takes everything left.
func (o *Order) consumeReservation(qty Quantity) int64 {
	unfilled := o.RemainingQuantity()
	if o.RemainingReserved <= 0 || unfilled == 0 {
		return 0
	}
	if qty >= unfilled {
		share := o.RemainingReserved
		o.RemainingReserved = 0
		return share
	}
	// floor(RemainingReserved * qty / unfilled) in 128-bit; qty < unfilled so Div64 cannot overflow.
	hi, lo := bits.Mul64(uint64(o.RemainingReserved), uint64(qty))
	share, _ := bits.Div64(hi, lo, uint64(unfilled))
	o.RemainingReserved -= int64(share)
	return int64(share)
}

// ApplyFill records an execution of qty and returns the reservation share it
// consumed (0 for orders without a cash reservation).
func (o *Order) ApplyFill(qty Quantity) int64 {
	released := o.consumeReservation(qty)
	o.FilledQuantity += qty
	o.UpdatedAt = time.Now()
	if o.FilledQuantity >= o.Quantity {
		o.Status = StatusFilled
	} else {
		o.Status = StatusPartiallyFilled
	}
	return released
}

// CloseRemainder terminates an order whose unfilled quantity will not rest:
// REJECTED when nothing executed, CANCELED when it was partially filled.
// RemainingReserved is left untouched for the caller to release.
func (o *Order) CloseRemainder() {
	if o.RemainingQuantity() == 0 {
		return
	}
	if o.FilledQuantity == 0 {
		o.Status = StatusRejected
	} else {
		o.Status = StatusCanceled
	}
	o.UpdatedAt = time.Now()
}

func (o *Order) Cancel() error {
	if o.Status == StatusFilled || o.Status == StatusCanceled {
		return common.ErrOrderCompleted
	}
	o.Status = StatusCanceled
	o.UpdatedAt = time.Now()
	return nil
}

// Snapshot returns a detached copy of the order (queue links cleared).
// Callers must hold the owning book's lock while taking it.
func (o *Order) Snapshot() *Order {
	c := *o
	c.Prev = nil
	c.Next = nil
	return &c
}

// State is the persisted form of an order, the input of Restore.
type State struct {
	ID, ClientOrderID, AccountID string
	Symbol                       Symbol
	Side                         OrderSide
	Type                         OrderType
	Price                        Price
	Quantity, FilledQuantity     Quantity
	Status                       OrderStatus
	ReservedAmount               int64
	RemainingReserved            int64
	CreatedAt, UpdatedAt         time.Time
}

// Restore rebuilds an order from persisted state so adapters never write the
// order's fields themselves. It rejects state no sequence of domain operations
// could have produced.
func Restore(s State) (*Order, error) {
	if !s.Side.IsValid() {
		return nil, common.ErrInvalidSide
	}
	if s.Type != TypeLimit && s.Type != TypeMarket {
		return nil, common.ErrInvalidOrderType
	}
	switch s.Status {
	case StatusNew, StatusPartiallyFilled, StatusFilled, StatusCanceled, StatusRejected:
	default:
		return nil, fmt.Errorf("%w: order %s status %q", common.ErrInvalidState, s.ID, s.Status)
	}
	if s.Quantity == 0 {
		return nil, common.ErrInvalidQuantity
	}
	if s.Price < 0 || (s.Type == TypeLimit && s.Price == 0) {
		return nil, common.ErrInvalidPrice
	}
	if s.FilledQuantity > s.Quantity || s.ReservedAmount < 0 ||
		s.RemainingReserved < 0 || s.RemainingReserved > s.ReservedAmount {
		return nil, fmt.Errorf("%w: order %s filled=%d/%d reserved=%d/%d", common.ErrInvalidState,
			s.ID, s.FilledQuantity, s.Quantity, s.RemainingReserved, s.ReservedAmount)
	}
	return &Order{
		ID: s.ID, ClientOrderID: s.ClientOrderID, AccountID: s.AccountID, Symbol: s.Symbol,
		Side: s.Side, Type: s.Type, Price: s.Price, Quantity: s.Quantity, FilledQuantity: s.FilledQuantity,
		Status: s.Status, CreatedAt: s.CreatedAt, UpdatedAt: s.UpdatedAt,
		ReservedAmount: s.ReservedAmount, RemainingReserved: s.RemainingReserved,
	}, nil
}

// IsOpen reports whether the order is resting (or may rest) in the book.
func (o *Order) IsOpen() bool {
	return o.Type == TypeLimit && o.RemainingQuantity() > 0 &&
		(o.Status == StatusNew || o.Status == StatusPartiallyFilled)
}
