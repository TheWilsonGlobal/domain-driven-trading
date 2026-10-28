package command

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

// RecoveryReport summarises what RecoverBook did at startup.
type RecoveryReport struct {
	Restored       int            // open orders put back into the book
	LocksCorrected int            // accounts whose persisted lock differed from their open orders
	OpenByAccount  map[string]int // open orders per account after recovery
}

// RecoverBook rebuilds the in-memory book from the persisted open orders and
// re-derives the reservations from them.
//
// The open orders (oldest first, i.e. time priority) go back into the matcher.
// Then, for every account owning an open order, the cash lock is recomputed as
// the sum of the BUY orders' RemainingReserved and the share lock as the sum of
// the SELL orders' remaining quantity. The orders are the source of truth for
// locks: a persisted lock that disagrees (a crash between the order write and
// the account write) is corrected and saved, and counted in the report. An open
// order whose account is missing aborts the start: serving with a book that has
// no owner would let it trade for nobody.
//
// Accounts without open orders are not touched. Call it after seeding and
// before serving.
func RecoverBook(
	ctx context.Context,
	m *order.Matcher,
	orders ports.OrderRepository,
	accounts ports.AccountRepository,
	logger *slog.Logger,
) (RecoveryReport, error) {
	if logger == nil {
		logger = slog.Default()
	}
	report := RecoveryReport{OpenByAccount: map[string]int{}}

	open, err := orders.ListOpen(ctx, m.Symbol())
	if err != nil {
		return report, fmt.Errorf("recovery: list open orders: %w", err)
	}

	type locks struct {
		cash  int64
		stock uint32
	}
	expected := map[string]*locks{}
	var accountIDs []string // first-seen order, for deterministic processing
	for _, o := range open {
		l, ok := expected[o.AccountID]
		if !ok {
			l = &locks{}
			expected[o.AccountID] = l
			accountIDs = append(accountIDs, o.AccountID)
		}
		if o.Side == order.SideBuy {
			l.cash += o.RemainingReserved
		} else {
			l.stock += o.RemainingQuantity().Uint32()
		}
		report.OpenByAccount[o.AccountID]++
	}

	err = m.Atomically(func() error {
		for _, id := range accountIDs {
			acc, err := resolveAccount(ctx, accounts, id)
			if err != nil {
				return fmt.Errorf("recovery: owner of open orders: %w", err)
			}
			want := expected[id]
			snap := acc.Snapshot()
			if snap.LockedCash == want.cash && snap.LockedStocks[m.Symbol()] == want.stock {
				continue
			}
			logger.Warn("recovery: persisted lock differs from open orders, correcting",
				"account", id,
				"locked_cash", snap.LockedCash, "expected_cash", want.cash,
				"locked_stock", snap.LockedStocks[m.Symbol()], "expected_stock", want.stock)
			if err := acc.RestoreLocks(want.cash, m.Symbol(), want.stock); err != nil {
				return fmt.Errorf("recovery: %w", err)
			}
			if err := accounts.Save(ctx, acc); err != nil {
				return fmt.Errorf("recovery: save corrected account %s: %w", id, err)
			}
			report.LocksCorrected++
		}
		if err := m.RestoreResting(open); err != nil {
			return fmt.Errorf("recovery: %w", err)
		}
		report.Restored = len(open)
		return nil
	})
	if err != nil {
		return report, err
	}
	logger.Info("recovery: book rebuilt", "orders", report.Restored, "accounts", len(accountIDs), "locks_corrected", report.LocksCorrected)
	return report, nil
}
