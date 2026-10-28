package account

import (
	"errors"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
)

// sumBy totals the amounts of one asset by entry type.
func sumBy(tx *Transaction, asset AssetType, typ EntryType) int64 {
	var n int64
	for _, e := range tx.Entries {
		if e.AssetType == asset && e.Type == typ {
			n += e.Amount
		}
	}
	return n
}

// 5.3: the journal must book exactly what SettleBuyTrade / SettleSellTrade move
// (the fee is rounded UP), credit FEE_COLLECTOR with both fees and balance per asset.
func TestJournal_MatchesSettlementAndBalances(t *testing.T) {
	cases := []struct {
		price common.Price
		qty   common.Quantity
	}{{19500, 3}, {19500, 100}, {10, 1}, {19490, 7}, {100_000_000, 1_000_000}}
	for _, c := range cases {
		tx, err := CreateTradeSettlementJournal("T1", "BUYER", "SELLER", FeeCollectorID, "APX", c.price, c.qty)
		if err != nil {
			t.Fatalf("%v x %v: %v", c.price, c.qty, err)
		}

		buyer, seller := NewAccount("BUYER", 1<<60), NewAccount("SELLER", 0)
		seller.CreditStock("APX", uint32(c.qty))
		if err := seller.ReserveSellHolding("APX", c.qty); err != nil {
			t.Fatal(err)
		}
		buyFee, err := buyer.SettleBuyTrade("APX", c.price, c.qty, 0)
		if err != nil {
			t.Fatal(err)
		}
		net, sellFee, err := seller.SettleSellTrade("APX", c.price, c.qty)
		if err != nil {
			t.Fatal(err)
		}

		byAcc := map[string]int64{}
		for _, e := range tx.Entries {
			if e.AssetType != AssetCash {
				continue
			}
			if e.Type == Credit {
				byAcc[e.AccountID] -= e.Amount
			} else {
				byAcc[e.AccountID] += e.Amount
			}
		}
		if got, want := -byAcc["BUYER"], (1<<60)-buyer.CashBalance; got != want {
			t.Errorf("%v x %v: buyer cash leg %d, settlement moved %d", c.price, c.qty, got, want)
		}
		if byAcc["SELLER"] != net {
			t.Errorf("%v x %v: seller cash leg %d, settlement credited %d", c.price, c.qty, byAcc["SELLER"], net)
		}
		if byAcc[FeeCollectorID] != buyFee+sellFee {
			t.Errorf("%v x %v: collector leg %d, fees %d", c.price, c.qty, byAcc[FeeCollectorID], buyFee+sellFee)
		}
		for _, asset := range []AssetType{AssetCash, AssetStock} {
			if d, cr := sumBy(tx, asset, Debit), sumBy(tx, asset, Credit); d != cr || d == 0 {
				t.Errorf("%v x %v: %s debits=%d credits=%d", c.price, c.qty, asset, d, cr)
			}
		}
	}
}

func TestJournal_OverflowRejected(t *testing.T) {
	_, err := CreateTradeSettlementJournal("T2", "B", "S", FeeCollectorID, "APX", common.Price(1<<62), common.Quantity(1<<30))
	if !errors.Is(err, common.ErrArithmeticOverflow) {
		t.Fatalf("overflow err=%v", err)
	}
}
