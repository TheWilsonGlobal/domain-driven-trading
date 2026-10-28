// Package bootstrap is the shared composition for cmd/gateway and cmd/engine:
// it builds the matching core (book, matcher, command and query handlers) from
// already-constructed ports, and seeds the resting maker quotes. It takes
// interfaces only, so it imports no infrastructure; each binary decides which
// adapters (PostgreSQL / memory, Kafka / in-memory bus) to hand in.
package bootstrap

import (
	"context"
	"log/slog"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/query"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/seed"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

// Symbol is the one book the system runs (decision D4).
const Symbol order.Symbol = "APX"

// Ports are the driven adapters the core needs. Orders and Journal are
// optional: when nil the handlers keep orders in the book only.
type Ports struct {
	Accounts  ports.AccountRepository
	Orders    ports.OrderRepository
	Journal   ports.TradeJournalRepository
	Publisher ports.EventPublisher
}

// Core is the wired matching core.
type Core struct {
	Symbol    order.Symbol
	Matcher   *order.Matcher
	Place     *command.PlaceOrderHandler
	Cancel    *command.CancelOrderHandler
	OrderBook *query.GetOrderBookHandler
	Balance   *query.GetBalanceHandler
}

// NewMatcher builds the empty APX book and its matcher. It is separate from
// NewCore because recovery needs the matcher before the handlers exist.
func NewMatcher() *order.Matcher {
	return order.NewMatcher(order.NewOrderBook(Symbol))
}

// NewCore wires the handlers around the matcher and the given ports.
func NewCore(matcher *order.Matcher, p Ports) *Core {
	place := command.NewPlaceOrderHandler(matcher, p.Accounts, p.Publisher)
	cancel := command.NewCancelOrderHandler(matcher, p.Accounts, p.Publisher)
	if p.Orders != nil {
		place = place.WithOrderStore(p.Orders, p.Journal)
		cancel = cancel.WithOrderStore(p.Orders)
	}
	return &Core{
		Symbol:    Symbol,
		Matcher:   matcher,
		Place:     place,
		Cancel:    cancel,
		OrderBook: query.NewGetOrderBookHandler(matcher),
		Balance:   query.NewGetBalanceHandler(p.Accounts),
	}
}

// MakerQuotes is the initial resting liquidity, shared by every binary.
func MakerQuotes() []seed.MakerOrder {
	return []seed.MakerOrder{
		{ClientOrderID: "SEED_BID_1", AccountID: "ACC_INST_MM", Side: order.SideBuy, Price: 19450, Quantity: 5000},
		{ClientOrderID: "SEED_ASK_1", AccountID: "ACC_INST_MM", Side: order.SideSell, Price: 19500, Quantity: 5000},
	}
}

// SeedMakers places the maker quotes once. A maker that already has open
// orders (restored by recovery, openByAccount) is skipped. Every Handle error
// is returned by seed.MakerOrders; none is dropped here.
func (c *Core) SeedMakers(ctx context.Context, openByAccount map[string]int, logger *slog.Logger) (int, error) {
	return seed.MakerOrders(ctx, c.Place, c.Symbol, openByAccount, MakerQuotes(), logger)
}
