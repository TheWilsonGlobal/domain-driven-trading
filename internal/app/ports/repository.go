package ports

import (
	"context"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

type AccountRepository interface {
	GetByID(ctx context.Context, accountID string) (*account.Account, error)
	Save(ctx context.Context, acc *account.Account) error
}

type OrderRepository interface {
	GetByID(ctx context.Context, orderID string) (*order.Order, error)
	Save(ctx context.Context, ord *order.Order) error
	UpdateStatus(ctx context.Context, orderID string, status order.OrderStatus, filledQty order.Quantity) error
}

type TradeRepository interface {
	SaveBatch(ctx context.Context, trades []order.OrderMatchedEvent) error
}

type LedgerRepository interface {
	SaveTransaction(ctx context.Context, tx *account.Transaction) error
}

type IdempotencyStore interface {
	CheckAndSet(ctx context.Context, key string, ttlSeconds int) (bool, error)
}
