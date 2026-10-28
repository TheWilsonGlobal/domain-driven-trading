package account

import (
	"errors"
	"math"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
)

// Plan 2.2: constants chosen so the largest legal order fits in int64.
func TestCalculateRequiredFunds_MaxLimitsFit(t *testing.T) {
	got, err := CalculateRequiredFunds(common.MaxPrice, common.MaxQty, DefaultFeeRateBasisPoints)
	if err != nil || got <= 0 {
		t.Fatalf("got=%d err=%v", got, err)
	}
	// protection price (ask * 1.1) for a MaxPrice ask must also fit
	if _, err := CalculateRequiredFunds(common.MaxPrice/10*11, common.MaxQty, DefaultFeeRateBasisPoints); err != nil {
		t.Fatalf("protection price overflow: %v", err)
	}
}

func TestCalculateRequiredFunds_Overflow(t *testing.T) {
	for _, tc := range []struct {
		p common.Price
		q common.Quantity
	}{
		{common.Price(math.MaxInt64), 2},
		{common.Price(math.MaxInt64 / 2), math.MaxUint32},
		{common.Price(1 << 50), math.MaxUint32},
	} {
		if _, err := CalculateRequiredFunds(tc.p, tc.q, DefaultFeeRateBasisPoints); !errors.Is(err, common.ErrArithmeticOverflow) {
			t.Errorf("price=%d qty=%d: err=%v, want overflow", tc.p, tc.q, err)
		}
	}
}

func TestReserveBuyFunds_OverflowLeavesAccountUntouched(t *testing.T) {
	a := NewAccount("A", math.MaxInt64)
	if _, err := a.ReserveBuyFunds(common.Price(math.MaxInt64/2), math.MaxUint32); !errors.Is(err, common.ErrArithmeticOverflow) {
		t.Fatalf("err = %v", err)
	}
	if a.LockedCash != 0 {
		t.Errorf("LockedCash = %d", a.LockedCash)
	}
}

// Plan 2.7: locked > holdings must not wrap the uint32 subtraction.
func TestReserveSellHolding_NoUnderflow(t *testing.T) {
	a := NewAccount("A", 0)
	a.Holdings["APX"] = 5
	a.LockedStocks["APX"] = 10 // inconsistent state; unchecked 5-10 wraps to ~4e9
	if err := a.ReserveSellHolding("APX", 1); !errors.Is(err, common.ErrInsufficientHolding) {
		t.Fatalf("err = %v, want ErrInsufficientHolding", err)
	}
	if a.LockedStocks["APX"] != 10 {
		t.Errorf("locked changed: %d", a.LockedStocks["APX"])
	}
}

func TestSettleSellTrade_MismatchReturnsError(t *testing.T) {
	a := NewAccount("A", 1000)
	a.Holdings["APX"] = 5
	a.LockedStocks["APX"] = 2 // only 2 locked but 5 sold
	if _, _, err := a.SettleSellTrade("APX", 19500, 5); !errors.Is(err, common.ErrSettlementMismatch) {
		t.Fatalf("err = %v, want ErrSettlementMismatch", err)
	}
	if a.CashBalance != 1000 || a.Holdings["APX"] != 5 || a.LockedStocks["APX"] != 2 {
		t.Errorf("state mutated on mismatch: %+v", a)
	}

	b := NewAccount("B", 0)
	b.Holdings["APX"] = 5
	b.LockedStocks["APX"] = 5
	cash, _, err := b.SettleSellTrade("APX", 20000, 5)
	if err != nil || cash != 100000-150 {
		t.Fatalf("cash=%d err=%v", cash, err)
	}
	if b.Holdings["APX"] != 0 || b.LockedStocks["APX"] != 0 {
		t.Errorf("not settled: %+v", b)
	}
}

// Plan 3.1: SettleBuyTrade debits the actual cost and unlocks only the given share.
func TestSettleBuyTrade_ActualCostAndShare(t *testing.T) {
	a := NewAccount("A", 1_000_000)
	a.LockedCash = 300_000
	fee, err := a.SettleBuyTrade("APX", 20000, 5, 120_000) // n=100_000, fee=150
	if err != nil || fee != 150 {
		t.Fatalf("fee=%d err=%v", fee, err)
	}
	if a.CashBalance != 1_000_000-100_150 || a.LockedCash != 180_000 || a.Holdings["APX"] != 5 {
		t.Errorf("state = %+v", a)
	}
}

func TestSettleBuyTrade_MismatchLeavesAccountUntouched(t *testing.T) {
	a := NewAccount("A", 1_000_000)
	a.LockedCash = 10
	if _, err := a.SettleBuyTrade("APX", 20000, 5, 120_000); !errors.Is(err, common.ErrSettlementMismatch) {
		t.Fatalf("err = %v", err)
	}
	if a.CashBalance != 1_000_000 || a.LockedCash != 10 || a.Holdings["APX"] != 0 {
		t.Errorf("mutated: %+v", a)
	}
}

// The fee rounds up: 1 VND notional-equivalent never costs 0.
func TestFee_RoundsUp(t *testing.T) {
	if f, err := Fee(10, 1); err != nil || f != 1 { // 10*15/10000 = 0.015 -> 1
		t.Fatalf("fee=%d err=%v", f, err)
	}
	if f, _ := Fee(20000, 5); f != 150 {
		t.Fatalf("exact fee = %d", f)
	}
}

func TestRelease_RefusesToOverRelease(t *testing.T) {
	a := NewAccount("A", 100)
	a.LockedCash = 5
	a.LockedStocks["APX"] = 2
	if err := a.ReleaseBuyFunds(6); !errors.Is(err, common.ErrSettlementMismatch) || a.LockedCash != 5 {
		t.Errorf("cash: err=%v locked=%d", err, a.LockedCash)
	}
	if err := a.ReleaseSellHolding("APX", 3); !errors.Is(err, common.ErrSettlementMismatch) || a.LockedStocks["APX"] != 2 {
		t.Errorf("shares: err=%v locked=%d", err, a.LockedStocks["APX"])
	}
}
