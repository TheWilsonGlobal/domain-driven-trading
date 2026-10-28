package command_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

// CONSERVATION (plan phase 3 gate): across every account including the fee
// collector, total cash and total shares are identical before and after a
// seeded random sequence of limit orders, partial fills, cancels (own and
// foreign) and market orders; locked amounts equal the reservations of the
// open orders; nothing goes negative.
func TestConservation_RandomSequence(t *testing.T) {
	for _, seed := range []int64{1, 2, 3, 42, 2026} {
		t.Run(fmt.Sprintf("seed-%d", seed), func(t *testing.T) { runConservation(t, seed, 3000) })
	}
}

func runConservation(t *testing.T, seed int64, steps int) {
	ids := []string{"A1", "A2", "A3", "A4"}
	var accs []*account.Account
	for _, id := range ids {
		accs = append(accs, trader(id, 5000))
	}
	e := newEngineEnv(accs...)
	all := append(append([]*account.Account{}, accs...), e.acc("FEE_COLLECTOR"))

	totals := func() (cash int64, shares uint64) {
		for _, a := range all {
			s := a.Snapshot()
			cash += s.CashBalance
			shares += uint64(s.Holdings["APX"])
		}
		return
	}
	cash0, shares0 := totals()

	rng := rand.New(rand.NewSource(seed))
	type placed struct{ id, owner string }
	var open []placed
	ctx := context.Background()

	for i := 0; i < steps; i++ {
		owner := ids[rng.Intn(len(ids))]
		switch r := rng.Intn(10); {
		case r < 6: // limit order in a tight band so most of them cross
			side := order.SideBuy
			if rng.Intn(2) == 0 {
				side = order.SideSell
			}
			price := order.Price(19900 + 10*rng.Intn(11))
			res, err := e.place.Handle(ctx, command.PlaceOrderCommand{
				AccountID: owner, Symbol: "APX", Side: side, Type: order.TypeLimit,
				Price: price, Quantity: order.Quantity(1 + rng.Intn(40)),
			})
			if err != nil {
				requireExpected(t, err)
				continue
			}
			open = append(open, placed{res.Order.ID, owner})
		case r < 8: // market order
			side := order.SideBuy
			if rng.Intn(2) == 0 {
				side = order.SideSell
			}
			_, err := e.place.Handle(ctx, command.PlaceOrderCommand{
				AccountID: owner, Symbol: "APX", Side: side, Type: order.TypeMarket,
				Quantity: order.Quantity(1 + rng.Intn(60)),
			})
			if err != nil {
				requireExpected(t, err)
			}
		default: // cancel: usually the owner, sometimes a stranger
			if len(open) == 0 {
				continue
			}
			k := rng.Intn(len(open))
			p := open[k]
			caller := p.owner
			foreign := rng.Intn(4) == 0
			if foreign {
				caller = ids[(indexOf(ids, p.owner)+1)%len(ids)]
			}
			_, err := e.cancelAs(caller, p.id)
			switch {
			case foreign:
				if !errors.Is(err, common.ErrOrderNotFound) {
					t.Fatalf("step %d: foreign cancel err = %v", i, err)
				}
			case err == nil:
				open = append(open[:k], open[k+1:]...)
			case errors.Is(err, common.ErrOrderNotFound):
				open = append(open[:k], open[k+1:]...) // already filled
			default:
				t.Fatalf("step %d: owner cancel: %v", i, err)
			}
		}
	}

	cash1, shares1 := totals()
	if cash0 != cash1 {
		t.Errorf("total cash changed: %d -> %d (delta %d)", cash0, cash1, cash1-cash0)
	}
	if shares0 != shares1 {
		t.Errorf("total shares changed: %d -> %d", shares0, shares1)
	}

	// locked_cash / locked_stocks must equal what the still-open orders hold.
	wantCash := map[string]int64{}
	wantShares := map[string]uint32{}
	depthSum := uint32(0)
	for _, p := range open {
		o, ok := e.m.Get(p.id)
		if !ok {
			continue // filled in the meantime
		}
		if o.Side == order.SideBuy {
			wantCash[o.AccountID] += o.RemainingReserved
		} else {
			wantShares[o.AccountID] += o.RemainingQuantity().Uint32()
		}
		depthSum += o.RemainingQuantity().Uint32()
	}
	for _, a := range all {
		s := a.Snapshot()
		if s.LockedCash != wantCash[s.ID] {
			t.Errorf("%s locked_cash = %d, open reservations = %d", s.ID, s.LockedCash, wantCash[s.ID])
		}
		if s.LockedStocks["APX"] != wantShares[s.ID] {
			t.Errorf("%s locked_stocks = %d, open SELL remaining = %d", s.ID, s.LockedStocks["APX"], wantShares[s.ID])
		}
		if s.CashBalance < 0 || s.LockedCash < 0 || s.LockedCash > s.CashBalance {
			t.Errorf("%s bad cash state: cash=%d locked=%d", s.ID, s.CashBalance, s.LockedCash)
		}
		if s.LockedStocks["APX"] > s.Holdings["APX"] {
			t.Errorf("%s locked stocks %d > holdings %d", s.ID, s.LockedStocks["APX"], s.Holdings["APX"])
		}
	}

	// book depth equals the sum of remaining quantities of the open orders (3.8).
	d := e.m.Depth(50)
	var bookQty uint32
	for _, l := range d.Bids {
		bookQty += l.TotalQuantity.Uint32()
	}
	for _, l := range d.Asks {
		bookQty += l.TotalQuantity.Uint32()
	}
	if len(d.Bids) < 50 && len(d.Asks) < 50 && bookQty != depthSum {
		t.Errorf("depth quantity = %d, sum of open remaining = %d", bookQty, depthSum)
	}
	if fc := e.acc("FEE_COLLECTOR").Snapshot().CashBalance; fc <= 0 {
		t.Errorf("no fees collected over %d steps (fc=%d); the sequence did not trade", steps, fc)
	}
}

func indexOf(ids []string, id string) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return 0
}

// requireExpected fails the test on any placement error that is not a normal
// business rejection.
func requireExpected(t *testing.T, err error) {
	t.Helper()
	switch {
	case errors.Is(err, common.ErrInsufficientBalance),
		errors.Is(err, common.ErrInsufficientHolding),
		errors.Is(err, common.ErrNoLiquidity):
	default:
		t.Fatalf("unexpected placement error: %v", err)
	}
}
