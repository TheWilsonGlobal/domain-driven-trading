package postgres

import (
	"context"
	"errors"
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
)

// 5.5: the adapters must not write domain state fields; they go through the
// domain Restore factories.
func TestAdapterSourceUsesDomainFactories(t *testing.T) {
	for _, f := range []string{"account_repository.go", "order_repository.go"} {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		if m := stateWrite.FindString(string(src)); m != "" {
			t.Errorf("%s writes domain state directly: %q", f, m)
		}
		for _, banned := range []string{"NewAccount(", "NewOrder(", ".CreditStock("} {
			if strings.Contains(string(src), banned) {
				t.Errorf("%s builds domain objects without Restore: %q", f, banned)
			}
		}
	}
}

// stateWrite matches an assignment to a domain state field (x.LockedCash = ...,
// x.Holdings[s] += ...); reads such as snap.Holdings[s] do not match.
var stateWrite = regexp.MustCompile(`\.(CashBalance|LockedCash|Holdings|LockedStocks|FilledQuantity|ReservedAmount|RemainingReserved|Status)(\[[^\]]*\])?\s*([+-]?=[^=])`)

func holdingRow(sym string, qty, locked int64) []any { return []any{sym, qty, locked} }

// 5.5 + 5.2: a loaded account carries its holdings AND locked stocks, built via account.Restore.
func TestGetByID_RestoresHoldingsAndLocks(t *testing.T) {
	d := &fakeDB{row: fakeRow{cash: 1000, lock: 200}, rows: [][]any{holdingRow("APX", 50, 20), holdingRow("HPG", 7, 0)}}
	acc, err := newRepository(d).GetByID(context.Background(), "ACC_X")
	if err != nil {
		t.Fatal(err)
	}
	s := acc.Snapshot()
	if s.Holdings["APX"] != 50 || s.LockedStocks["APX"] != 20 || s.Holdings["HPG"] != 7 || s.LockedCash != 200 {
		t.Fatalf("restored %+v", s)
	}
}

func TestGetByID_InconsistentHoldingIsAnError(t *testing.T) {
	d := &fakeDB{row: fakeRow{cash: 1000}, rows: [][]any{holdingRow("APX", 5, 9)}} // locked > held
	_, err := newRepository(d).GetByID(context.Background(), "ACC_X")
	if !errors.Is(err, common.ErrInvalidState) {
		t.Fatalf("err=%v want ErrInvalidState", err)
	}
}

// 5.1 + 5.2: Save writes the account row and one account_holdings row per
// symbol (quantity and locked quantity) in one transaction.
func TestSave_WritesHoldingsInOneTransaction(t *testing.T) {
	d := &fakeDB{}
	repo := newRepository(d)
	acc := account.NewAccount("ACC_X", 500)
	acc.CreditStock("APX", 100)
	acc.CreditStock("HPG", 5)
	if err := acc.ReserveSellHolding("APX", 30); err != nil {
		t.Fatal(err)
	}
	if err := repo.Save(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
	if len(d.txs) != 1 || !d.txs[0].committed {
		t.Fatalf("txs=%d, want one committed transaction", len(d.txs))
	}
	var accRows, holdRows int
	locked := map[string]int64{}
	for _, e := range d.committed {
		switch {
		case strings.Contains(e.sql, "INTO accounts"):
			accRows++
		case strings.Contains(e.sql, "INTO account_holdings"):
			holdRows++
			locked[e.args[1].(string)] = e.args[3].(int64)
		}
	}
	if accRows != 1 || holdRows != 2 || locked["APX"] != 30 || locked["HPG"] != 0 {
		t.Fatalf("accRows=%d holdRows=%d locked=%v", accRows, holdRows, locked)
	}
}

// 5.2: seed -> reload. A fresh repository (a restart) over the same store sees
// the starter holdings the seeding step saved.
func TestSeedThenReload_ShowsHoldings(t *testing.T) {
	d := &fakeDB{}
	acc := account.NewAccount("ACC_SEED", 700)
	acc.CreditStock("APX", 10000)
	if err := newRepository(d).Save(context.Background(), acc); err != nil {
		t.Fatal(err)
	}
	// "restart": replay the committed rows as what a new process would read
	var rows [][]any
	for _, e := range d.committed {
		if strings.Contains(e.sql, "INTO account_holdings") {
			rows = append(rows, holdingRow(e.args[1].(string), e.args[2].(int64), e.args[3].(int64)))
		}
	}
	reload := &fakeDB{row: fakeRow{cash: 700}, rows: rows}
	got, err := newRepository(reload).GetByID(context.Background(), "ACC_SEED")
	if err != nil || got.Snapshot().Holdings["APX"] != 10000 {
		t.Fatalf("reloaded %+v err=%v, want 10000 APX", got, err)
	}
}

// 5.1: Save errors are returned (the old code discarded them), the transaction
// is rolled back and the cache is untouched.
func TestSave_ErrorsAreReturnedAndRolledBack(t *testing.T) {
	boom := errors.New("write failed")
	for _, failAt := range []int{1, 2} {
		d := &fakeDB{failAt: failAt, txFailErr: boom}
		repo := newRepository(d)
		acc := account.NewAccount("ACC_X", 500)
		acc.CreditStock("APX", 1)
		if err := repo.Save(context.Background(), acc); !errors.Is(err, boom) {
			t.Fatalf("failAt=%d err=%v, want the write error", failAt, err)
		}
		if len(d.committed) != 0 || !d.txs[0].rolledBack {
			t.Errorf("failAt=%d: committed=%d rolledBack=%v", failAt, len(d.committed), d.txs[0].rolledBack)
		}
		if _, cached := repo.memoryAccs["ACC_X"]; cached {
			t.Errorf("failAt=%d: failed save was cached", failAt)
		}
	}
	d := &fakeDB{beginErr: boom}
	if err := newRepository(d).Save(context.Background(), account.NewAccount("A", 1)); !errors.Is(err, boom) {
		t.Fatalf("begin err=%v", err)
	}
}

// 5.1: Save never swaps the cached pointer; a different object with the same id
// is copied into the live one.
func TestSave_UpdatesCachedAccountInPlace(t *testing.T) {
	repo := newRepository(&fakeDB{})
	live := account.NewAccount("ACC_X", 100)
	if err := repo.Save(context.Background(), live); err != nil {
		t.Fatal(err)
	}
	fresh := account.NewAccount("ACC_X", 900)
	fresh.CreditStock("APX", 3)
	if err := repo.Save(context.Background(), fresh); err != nil {
		t.Fatal(err)
	}
	got, _ := repo.GetByID(context.Background(), "ACC_X")
	if got != live {
		t.Fatal("cached pointer was swapped")
	}
	if s := live.Snapshot(); s.CashBalance != 900 || s.Holdings["APX"] != 3 {
		t.Fatalf("live account not updated in place: %+v", s)
	}
}

// 5.1: the atomic conditional reserve.
func TestReserveCash_ConditionalUpdate(t *testing.T) {
	d := &fakeDB{tag: "UPDATE 1"}
	repo := newRepository(d)
	if err := repo.ReserveCash(context.Background(), "ACC_X", 250); err != nil {
		t.Fatal(err)
	}
	e := d.log[0]
	for _, want := range []string{"UPDATE accounts SET locked_cash = locked_cash + $1", "WHERE account_id = $2", "cash_balance - locked_cash >= $1"} {
		if !strings.Contains(e.sql, want) {
			t.Errorf("SQL lacks %q: %s", want, e.sql)
		}
	}
	if e.args[0] != int64(250) || e.args[1] != "ACC_X" {
		t.Errorf("args=%v", e.args)
	}

	d.tag = "UPDATE 0" // the WHERE refused: not enough free cash
	if err := repo.ReserveCash(context.Background(), "ACC_X", 250); !errors.Is(err, common.ErrInsufficientBalance) {
		t.Fatalf("0 rows: err=%v want ErrInsufficientBalance", err)
	}
	boom := errors.New("conn")
	d.execErr = boom
	if err := repo.ReserveCash(context.Background(), "ACC_X", 250); !errors.Is(err, boom) {
		t.Fatalf("exec error swallowed: %v", err)
	}
	if err := repo.ReserveCash(context.Background(), "ACC_X", 0); err == nil {
		t.Fatal("zero reserve accepted")
	}
}
