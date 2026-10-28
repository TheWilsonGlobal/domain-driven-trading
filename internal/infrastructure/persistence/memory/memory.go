// Package memory holds the in-memory implementations of the persistence ports.
// They back STORE=memory (chosen explicitly and logged by the composition root)
// and tests; nothing here survives a restart.
package memory

import (
	"context"
	"sync"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

var (
	_ ports.OrderRepository        = (*OrderRepository)(nil)
	_ ports.TradeJournalRepository = (*TradeJournalRepository)(nil)
)

// OrderRepository keeps detached copies of orders; ListOpen returns them in the
// order they were first saved (time priority), like the SQL seq column.
type OrderRepository struct {
	mu     sync.Mutex
	orders map[string]*order.Order
	seq    []string
}

func NewOrderRepository() *OrderRepository {
	return &OrderRepository{orders: map[string]*order.Order{}}
}

func (r *OrderRepository) GetByID(_ context.Context, id string) (*order.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[id]
	if !ok {
		return nil, common.ErrOrderNotFound
	}
	return o.Snapshot(), nil
}

func (r *OrderRepository) Save(_ context.Context, o *order.Order) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.orders[o.ID]; !ok {
		r.seq = append(r.seq, o.ID)
	}
	r.orders[o.ID] = o.Snapshot()
	return nil
}

func (r *OrderRepository) ListOpen(_ context.Context, symbol order.Symbol) ([]*order.Order, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*order.Order
	for _, id := range r.seq {
		if o := r.orders[id]; o.Symbol == symbol && o.IsOpen() {
			out = append(out, o.Snapshot())
		}
	}
	return out, nil
}

// TradeJournalRepository stores trades and journals; a batch is validated first
// and then applied as a whole, mirroring the all-or-nothing SQL transaction.
type TradeJournalRepository struct {
	mu      sync.Mutex
	trades  map[string]order.OrderMatchedEvent
	journal map[string]*account.Transaction
}

func NewTradeJournalRepository() *TradeJournalRepository {
	return &TradeJournalRepository{trades: map[string]order.OrderMatchedEvent{}, journal: map[string]*account.Transaction{}}
}

func (r *TradeJournalRepository) SaveTradesWithJournal(_ context.Context, items []ports.TradeWithJournal) error {
	for _, it := range items {
		if it.Journal == nil || len(it.Journal.Entries) == 0 {
			return common.ErrUnbalancedLedger
		}
		sums := map[account.AssetType]int64{}
		for _, e := range it.Journal.Entries {
			if e.Amount <= 0 {
				return common.ErrNegativeLedgerAmount
			}
			if e.Type == account.Debit {
				sums[e.AssetType] += e.Amount
			} else {
				sums[e.AssetType] -= e.Amount
			}
		}
		for _, net := range sums {
			if net != 0 {
				return common.ErrUnbalancedLedger
			}
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, it := range items {
		r.trades[it.Trade.TradeID] = it.Trade
		r.journal[it.Journal.ID] = it.Journal
	}
	return nil
}

// Counts reports how many trades and journal transactions are stored.
func (r *TradeJournalRepository) Counts() (trades, journals int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.trades), len(r.journal)
}
