package order

import (
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

	// Pointers for O(1) doubly-linked FIFO order queue at a single price level
	Prev *Order `json:"-"`
	Next *Order `json:"-"`
}

func NewOrder(id, clientOrderID, accountID string, symbol Symbol, side OrderSide, orderType OrderType, price Price, qty Quantity) (*Order, error) {
	if !side.IsValid() {
		return nil, common.ErrInvalidPrice
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

func (o *Order) ApplyFill(qty Quantity) {
	o.FilledQuantity += qty
	o.UpdatedAt = time.Now()
	if o.FilledQuantity >= o.Quantity {
		o.Status = StatusFilled
	} else {
		o.Status = StatusPartiallyFilled
	}
}

func (o *Order) Cancel() error {
	if o.Status == StatusFilled || o.Status == StatusCanceled {
		return common.ErrOrderCompleted
	}
	o.Status = StatusCanceled
	o.UpdatedAt = time.Now()
	return nil
}
