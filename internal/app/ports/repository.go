package ports

import (
	"context"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

type AccountRepository interface {
	GetByID(ctx context.Context, accountID string) (*account.Account, error)
	Save(ctx context.Context, acc *account.Account) error
}

// CashReserver is an optional capability of an AccountRepository: it persists
// a cash lock with one atomic conditional write (the store re-checks that
// cash_balance - locked_cash >= amount) and returns common.ErrInsufficientBalance
// when the store refuses. The in-memory lock has already been taken by the
// caller; on error the caller undoes it.
type CashReserver interface {
	ReserveCash(ctx context.Context, accountID string, amount int64) error
}

// OrderRepository persists orders so the book can be rebuilt after a restart.
type OrderRepository interface {
	// GetByID returns common.ErrOrderNotFound for an unknown id.
	GetByID(ctx context.Context, orderID string) (*order.Order, error)
	// Save inserts or updates the order (status, filled quantity, reservation).
	Save(ctx context.Context, ord *order.Order) error
	// ListOpen returns the NEW / PARTIALLY_FILLED limit orders of a symbol in
	// time priority (the order they were first saved), oldest first.
	ListOpen(ctx context.Context, symbol order.Symbol) ([]*order.Order, error)
}

// TradeWithJournal is one execution and its balanced double-entry voucher.
type TradeWithJournal struct {
	Trade   order.OrderMatchedEvent
	Journal *account.Transaction
}

// TradeJournalRepository is the unit of work for executions: it writes every
// trade and its journal entries in ONE transaction, so a trade never exists
// without its ledger legs or the other way round. Writes are idempotent per
// trade id / tx id.
type TradeJournalRepository interface {
	SaveTradesWithJournal(ctx context.Context, items []TradeWithJournal) error
}

type TradeRepository interface {
	SaveBatch(ctx context.Context, trades []order.OrderMatchedEvent) error
}

type LedgerRepository interface {
	SaveTransaction(ctx context.Context, tx *account.Transaction) error
}

// IdempotencyRecord is what the store keeps for one idempotency key: the
// fingerprint of the request that claimed it and, once the request finished
// successfully, the response (status + body) to replay.
type IdempotencyRecord struct {
	Fingerprint string
	Done        bool
	Status      int
	Body        []byte
}

// IdempotencyStore makes a request execute at most once per key.
//
//   - Claim atomically takes the key. claimed=true means the caller owns it and
//     must call Complete (success) or Release (failure). claimed=false returns
//     the existing record: Done=false means another request is still running.
//     A store error means NOTHING was claimed; callers must fail the request.
//   - The claim of a request that never finishes lapses after pendingTTL.
type IdempotencyStore interface {
	Claim(ctx context.Context, key, fingerprint string, pendingTTL time.Duration) (claimed bool, existing IdempotencyRecord, err error)
	Complete(ctx context.Context, key string, rec IdempotencyRecord, ttl time.Duration) error
	Release(ctx context.Context, key string) error
}
