package command

import (
	"context"
	"errors"
	"fmt"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

// joinErrs returns the only error unchanged (so identity checks keep working),
// or errors.Join of several.
func joinErrs(errs []error) error {
	if len(errs) == 1 {
		return errs[0]
	}
	return errors.Join(errs...)
}

// journalFor builds the balanced double-entry voucher of one execution.
func journalFor(ev order.OrderMatchedEvent) (*account.Transaction, error) {
	buyer, seller := ev.MakerAccountID, ev.TakerAccountID
	if ev.TakerSide == order.SideBuy {
		buyer, seller = ev.TakerAccountID, ev.MakerAccountID
	}
	return account.CreateTradeSettlementJournal(ev.TradeID, buyer, seller, account.FeeCollectorID, ev.Symbol, ev.Price, ev.Quantity)
}

// persistExecution writes everything one command changed: the touched accounts
// (cash, holdings, locks), the taker and maker orders, and the trades with their
// journals in a single transaction. Every step is attempted even when an earlier
// one failed, and all errors are returned together: the in-memory execution has
// already happened and cannot be rolled back.
//
// Known residual risk (documented in the report): accounts, orders and
// trade+journal are three separate commits, so a crash between them can leave the
// store partially updated. Recovery re-derives the locks from the open orders;
// a single unit of work over all three would close the window.
func persistExecution(
	ctx context.Context,
	accounts ports.AccountRepository,
	orders ports.OrderRepository,
	journal ports.TradeJournalRepository,
	touched []*account.Account,
	taker *order.Order,
	matches []order.OrderMatchedEvent,
) error {
	var errs []error
	seen := map[string]bool{}
	for _, acc := range touched {
		if acc == nil || seen[acc.ID] {
			continue
		}
		seen[acc.ID] = true
		if err := accounts.Save(ctx, acc); err != nil {
			errs = append(errs, fmt.Errorf("save account %s: %w", acc.ID, err))
		}
	}

	if orders != nil {
		if taker != nil {
			if err := orders.Save(ctx, taker); err != nil {
				errs = append(errs, fmt.Errorf("save order %s: %w", taker.ID, err))
			}
		}
		errs = append(errs, persistMakers(ctx, orders, matches)...)
	}

	if journal != nil && len(matches) > 0 {
		items := make([]ports.TradeWithJournal, 0, len(matches))
		for _, ev := range matches {
			tx, err := journalFor(ev)
			if err != nil {
				errs = append(errs, fmt.Errorf("journal %s: %w", ev.TradeID, err))
				continue
			}
			items = append(items, ports.TradeWithJournal{Trade: ev, Journal: tx})
		}
		if len(items) > 0 {
			if err := journal.SaveTradesWithJournal(ctx, items); err != nil {
				errs = append(errs, fmt.Errorf("save trades with journal: %w", err))
			}
		}
	}
	return errors.Join(errs...)
}

// persistMakers replays the executed fills on the stored copy of every maker
// order and saves it. ApplyFill is deterministic, so the result equals the live
// order the matcher just updated; a different reservation share means the store
// and the book disagree and is reported.
func persistMakers(ctx context.Context, orders ports.OrderRepository, matches []order.OrderMatchedEvent) []error {
	var errs []error
	stored := map[string]*order.Order{}
	var ids []string
	for _, ev := range matches {
		mk, ok := stored[ev.MakerOrderID]
		if !ok {
			var err error
			if mk, err = orders.GetByID(ctx, ev.MakerOrderID); err != nil {
				errs = append(errs, fmt.Errorf("load maker order %s: %w", ev.MakerOrderID, err))
				continue
			}
			stored[ev.MakerOrderID] = mk
			ids = append(ids, ev.MakerOrderID)
		}
		if unlock := mk.ApplyFill(ev.Quantity); unlock != ev.MakerUnlock {
			errs = append(errs, fmt.Errorf("maker order %s: stored reservation share %d != executed %d: %w",
				mk.ID, unlock, ev.MakerUnlock, common.ErrSettlementMismatch))
		}
	}
	for _, id := range ids {
		if err := orders.Save(ctx, stored[id]); err != nil {
			errs = append(errs, fmt.Errorf("save order %s: %w", id, err))
		}
	}
	return errs
}

// persistReserve saves a reservation: a BUY goes through the atomic conditional
// ReserveCash when the repository offers it, everything else (SELL share locks,
// repositories without the capability) through Save.
func persistReserve(ctx context.Context, repo ports.AccountRepository, acc *account.Account, side order.OrderSide, reserved int64) error {
	if side == order.SideBuy {
		if cr, ok := repo.(ports.CashReserver); ok {
			return cr.ReserveCash(ctx, acc.ID, reserved)
		}
	}
	return repo.Save(ctx, acc)
}
