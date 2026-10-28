package command

import (
	"context"
	"fmt"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

// resolveAccount loads an account and turns "no such account" (nil, nil) into
// ErrAccountNotFound so no caller can mistake it for success.
func resolveAccount(ctx context.Context, repo ports.AccountRepository, id string) (*account.Account, error) {
	acc, err := repo.GetByID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("load account %s: %w", id, err)
	}
	if acc == nil {
		return nil, fmt.Errorf("%w: %s", common.ErrAccountNotFound, id)
	}
	return acc, nil
}

// resolveFeeCollector fails loudly when the fee account is missing: fees would
// otherwise vanish and break cash conservation.
func resolveFeeCollector(ctx context.Context, repo ports.AccountRepository) (*account.Account, error) {
	acc, err := resolveAccount(ctx, repo, account.FeeCollectorID)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", common.ErrFeeCollectorMissing, err)
	}
	return acc, nil
}

// settleMatches settles BOTH sides of every execution (cash, shares, unlock)
// and credits the fees of both sides to the fee collector. takerAcc is the
// already-resolved taker account; maker accounts are resolved up front so a
// repository failure cannot interrupt the settlement half-way. It returns the
// distinct maker accounts so the caller can persist them.
func settleMatches(
	ctx context.Context,
	repo ports.AccountRepository,
	collector, takerAcc *account.Account,
	matches []order.OrderMatchedEvent,
) ([]*account.Account, error) {
	makers := make(map[string]*account.Account, len(matches))
	var makerList []*account.Account
	for _, ev := range matches {
		if _, ok := makers[ev.MakerAccountID]; ok {
			continue
		}
		acc, err := resolveAccount(ctx, repo, ev.MakerAccountID)
		if err != nil {
			return nil, err
		}
		makers[ev.MakerAccountID] = acc
		makerList = append(makerList, acc)
	}

	for _, ev := range matches {
		maker := makers[ev.MakerAccountID]
		buyer, seller, unlock := maker, takerAcc, ev.MakerUnlock
		if ev.TakerSide == order.SideBuy {
			buyer, seller, unlock = takerAcc, maker, ev.TakerUnlock
		}
		buyFee, err := buyer.SettleBuyTrade(ev.Symbol, ev.Price, ev.Quantity, unlock)
		if err != nil {
			return makerList, fmt.Errorf("settle buy %s: %w", ev.TradeID, err)
		}
		_, sellFee, err := seller.SettleSellTrade(ev.Symbol, ev.Price, ev.Quantity)
		if err != nil {
			return makerList, fmt.Errorf("settle sell %s: %w", ev.TradeID, err)
		}
		collector.DepositCash(buyFee + sellFee)
	}
	return makerList, nil
}

// canReleaseOrder reports whether the order's outstanding reservation can be
// handed back to acc (without doing it).
func canReleaseOrder(acc *account.Account, o *order.Order) bool {
	if o.Side == order.SideBuy {
		return acc.CanReleaseBuyFunds(o.RemainingReserved)
	}
	return acc.CanReleaseSellHolding(o.Symbol, o.RemainingQuantity())
}

// releaseOrder hands back exactly what the order still has reserved: the
// remaining cash share for a BUY, the unfilled shares for a SELL.
func releaseOrder(acc *account.Account, o *order.Order) error {
	if o.Side == order.SideBuy {
		return acc.ReleaseBuyFunds(o.RemainingReserved)
	}
	return acc.ReleaseSellHolding(o.Symbol, o.RemainingQuantity())
}

// isTerminal reports whether a status can never trade again.
func isTerminal(s order.OrderStatus) bool {
	return s == order.StatusCanceled || s == order.StatusRejected
}
