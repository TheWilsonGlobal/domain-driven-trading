package bootstrap_test

import (
	"context"
	"log/slog"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/query"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/seed"
	"github.com/domain-driven-trading/domain-driven-trading/internal/bootstrap"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/kafka"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/persistence/memory"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/persistence/postgres"
)

func newCore(t *testing.T, accounts []seed.Account) *bootstrap.Core {
	t.Helper()
	repo := postgres.NewPostgresAccountRepository(nil)
	if accounts != nil {
		if _, err := seed.Accounts(context.Background(), repo, accounts, slog.Default()); err != nil {
			t.Fatal(err)
		}
	}
	pub, _ := kafka.NewInMemoryPublisher()
	t.Cleanup(func() { _ = pub.Close() })
	return bootstrap.NewCore(bootstrap.NewMatcher(), bootstrap.Ports{
		Accounts:  repo,
		Orders:    memory.NewOrderRepository(),
		Journal:   memory.NewTradeJournalRepository(),
		Publisher: pub,
	})
}

func TestSeedMakers_PlacesQuotesOnceAndRestartSkips(t *testing.T) {
	c := newCore(t, seed.Defaults())
	n, err := c.SeedMakers(context.Background(), nil, slog.Default())
	if err != nil || n != 2 {
		t.Fatalf("placed=%d err=%v, want 2", n, err)
	}
	d, err := c.OrderBook.Handle(context.Background(), query.GetOrderBookQuery{Symbol: bootstrap.Symbol, MaxLevels: 5})
	if err != nil || len(d.Bids) != 1 || len(d.Asks) != 1 {
		t.Fatalf("depth=%+v err=%v", d, err)
	}
	n, err = c.SeedMakers(context.Background(), map[string]int{"ACC_INST_MM": 2}, slog.Default())
	if err != nil || n != 0 {
		t.Fatalf("restart placed=%d err=%v, want 0", n, err)
	}
}

func TestSeedMakers_HandleErrorIsReturned(t *testing.T) {
	// The maker has cash but no APX holding: the SELL quote is rejected by Handle
	// and that error must reach the caller (it was once dropped).
	c := newCore(t, []seed.Account{{ID: "ACC_INST_MM", Cash: 20_000_000_000}})
	if _, err := c.SeedMakers(context.Background(), nil, slog.Default()); err == nil {
		t.Fatal("expected the Handle error to surface, got nil")
	}
}
