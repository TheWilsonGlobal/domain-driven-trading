package account

import (
	"errors"
	"testing"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
)

// Buy lifecycle: reserve, partial fill (unlock only that fill's share),
// cancel releases exactly the remainder. Cash is conserved: balance drops by
// the executed cost only and nothing stays locked.
func TestBuyLifecycle_PartialFillThenCancel(t *testing.T) {
	a := NewAccount("A", 10_000_000)
	reserved, err := a.ReserveBuyFunds(20000, 100)
	if err != nil {
		t.Fatal(err)
	}
	if a.LockedCash != reserved || a.AvailableCash() != 10_000_000-reserved {
		t.Fatalf("locked=%d available=%d reserved=%d", a.LockedCash, a.AvailableCash(), reserved)
	}

	fill, _ := Fee(20000, 40)
	fillShare := 20000*40 + fill
	fee, err := a.SettleBuyTrade("APX", 20000, 40, fillShare)
	if err != nil || fee != fill {
		t.Fatalf("fee=%d err=%v", fee, err)
	}
	remainder := reserved - fillShare
	if a.LockedCash != remainder || a.Holdings["APX"] != 40 {
		t.Fatalf("after fill: locked=%d want %d holdings=%d", a.LockedCash, remainder, a.Holdings["APX"])
	}

	if !a.CanReleaseBuyFunds(remainder) || a.CanReleaseBuyFunds(remainder+1) || a.CanReleaseBuyFunds(-1) {
		t.Fatal("CanReleaseBuyFunds disagrees with the lock")
	}
	if err := a.ReleaseBuyFunds(remainder); err != nil {
		t.Fatal(err)
	}
	if a.LockedCash != 0 || a.CashBalance != 10_000_000-fillShare {
		t.Fatalf("after cancel: locked=%d cash=%d", a.LockedCash, a.CashBalance)
	}
}

func TestReserveBuyFunds_InsufficientAndInvalid(t *testing.T) {
	a := NewAccount("A", 1000)
	if _, err := a.ReserveBuyFunds(20000, 1); !errors.Is(err, common.ErrInsufficientBalance) {
		t.Fatalf("err = %v", err)
	}
	if _, err := a.ReserveBuyFunds(0, 1); !errors.Is(err, common.ErrInvalidPrice) {
		t.Fatalf("zero price err = %v (must never reserve 0)", err)
	}
	if a.LockedCash != 0 {
		t.Fatalf("locked = %d after rejected reserves", a.LockedCash)
	}
	// Locked cash is not available for a second order.
	b := NewAccount("B", 100_150)
	if _, err := b.ReserveBuyFunds(1000, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := b.ReserveBuyFunds(10, 1); !errors.Is(err, common.ErrInsufficientBalance) {
		t.Fatalf("second reserve err = %v", err)
	}
	if b.AvailableCash() != 0 {
		t.Fatalf("available = %d", b.AvailableCash())
	}
}

func TestSellLifecycle_ReserveSettlePartialThenRelease(t *testing.T) {
	a := NewAccount("A", 0)
	a.CreditStock("APX", 100)
	if err := a.ReserveSellHolding("APX", 100); err != nil {
		t.Fatal(err)
	}
	if err := a.ReserveSellHolding("APX", 1); !errors.Is(err, common.ErrInsufficientHolding) {
		t.Fatalf("over-reserve err = %v", err)
	}
	net, fee, err := a.SettleSellTrade("APX", 20000, 30)
	if err != nil {
		t.Fatal(err)
	}
	if net+fee != 20000*30 || a.CashBalance != net || a.Holdings["APX"] != 70 || a.LockedStocks["APX"] != 70 {
		t.Fatalf("net=%d fee=%d state=%+v", net, fee, a.Snapshot())
	}
	if !a.CanReleaseSellHolding("APX", 70) || a.CanReleaseSellHolding("APX", 71) {
		t.Fatal("CanReleaseSellHolding disagrees with the lock")
	}
	if err := a.ReleaseSellHolding("APX", 70); err != nil {
		t.Fatal(err)
	}
	if a.LockedStocks["APX"] != 0 || a.Holdings["APX"] != 70 {
		t.Fatalf("after cancel: %+v", a.Snapshot())
	}
	if err := a.ReleaseSellHolding("APX", 1); !errors.Is(err, common.ErrSettlementMismatch) {
		t.Fatalf("over-release err = %v", err)
	}
}

func TestDepositCash_AndAvailableCashFloor(t *testing.T) {
	a := NewAccount("A", 0)
	a.DepositCash(500)
	if a.CashBalance != 500 || a.AvailableCash() != 500 {
		t.Fatalf("%+v", a.Snapshot())
	}
	a.LockedCash = 900 // inconsistent books must not yield negative buying power
	if a.AvailableCash() != 0 {
		t.Fatalf("available = %d, want 0", a.AvailableCash())
	}
}

func TestRestore_RejectsStateNoOperationCouldProduce(t *testing.T) {
	now := time.Now()
	if _, err := Restore("A", -1, 0, nil, nil, now); !errors.Is(err, common.ErrInvalidState) {
		t.Errorf("negative cash err = %v", err)
	}
	if _, err := Restore("A", 0, -1, nil, nil, now); !errors.Is(err, common.ErrInvalidState) {
		t.Errorf("negative lock err = %v", err)
	}
	h := map[common.Symbol]uint32{"APX": 5}
	if _, err := Restore("A", 0, 0, h, map[common.Symbol]uint32{"APX": 6}, now); !errors.Is(err, common.ErrInvalidState) {
		t.Errorf("lock > holding err = %v", err)
	}
	a, err := Restore("A", 100, 40, h, map[common.Symbol]uint32{"APX": 5}, now)
	if err != nil || a.LockedCash != 40 || a.LockedStocks["APX"] != 5 {
		t.Fatalf("a=%+v err=%v", a, err)
	}
	h["APX"] = 99 // the restored account owns its maps
	if a.Holdings["APX"] != 5 {
		t.Error("Restore aliases the caller's holdings map")
	}
}

func TestSnapshot_IsDetached(t *testing.T) {
	a := NewAccount("A", 10)
	a.CreditStock("APX", 3)
	s := a.Snapshot()
	a.CreditStock("APX", 4)
	a.DepositCash(5)
	if s.Holdings["APX"] != 3 || s.CashBalance != 10 {
		t.Fatalf("snapshot moved with the live account: %+v", s)
	}
}

func TestReplaceWith_InPlaceAndIDChecked(t *testing.T) {
	live := NewAccount("A", 10)
	src := NewAccount("A", 99)
	src.CreditStock("APX", 7)
	if err := live.ReplaceWith(src); err != nil {
		t.Fatal(err)
	}
	if live.CashBalance != 99 || live.Holdings["APX"] != 7 {
		t.Fatalf("%+v", live.Snapshot())
	}
	src.CreditStock("APX", 1) // not aliased
	if live.Holdings["APX"] != 7 {
		t.Error("ReplaceWith aliases src maps")
	}
	if err := live.ReplaceWith(NewAccount("B", 1)); !errors.Is(err, common.ErrInvalidState) {
		t.Fatalf("id mismatch err = %v", err)
	}
	if live.CashBalance != 99 {
		t.Error("account changed on a rejected replace")
	}
}

func TestRestoreLocks(t *testing.T) {
	a := NewAccount("A", 100)
	a.CreditStock("APX", 10)
	if err := a.RestoreLocks(40, "APX", 6); err != nil || a.LockedCash != 40 || a.LockedStocks["APX"] != 6 {
		t.Fatalf("err=%v %+v", err, a.Snapshot())
	}
	if err := a.RestoreLocks(0, "APX", 0); err != nil {
		t.Fatal(err)
	}
	if _, ok := a.LockedStocks["APX"]; ok || a.LockedCash != 0 {
		t.Fatalf("zero locks not cleared: %+v", a.Snapshot())
	}
	if err := a.RestoreLocks(0, "APX", 11); !errors.Is(err, common.ErrInvalidState) {
		t.Fatalf("lock above holding err = %v", err)
	}
	if err := a.RestoreLocks(-1, "APX", 0); !errors.Is(err, common.ErrInvalidState) {
		t.Fatalf("negative cash lock err = %v", err)
	}
}
