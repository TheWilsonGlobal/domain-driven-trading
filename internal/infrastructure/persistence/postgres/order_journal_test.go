package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/jackc/pgx/v5"
)

func sampleTrade(t *testing.T, id string) ports.TradeWithJournal {
	t.Helper()
	ev := order.OrderMatchedEvent{
		TradeID: id, Symbol: "APX", MakerOrderID: "O1", TakerOrderID: "O2",
		MakerAccountID: "BUYER", TakerAccountID: "SELLER", TakerSide: order.SideSell,
		Price: 19500, Quantity: 3, Timestamp: time.Now(),
	}
	tx, err := account.CreateTradeSettlementJournal(id, "BUYER", "SELLER", account.FeeCollectorID, "APX", ev.Price, ev.Quantity)
	if err != nil {
		t.Fatal(err)
	}
	return ports.TradeWithJournal{Trade: ev, Journal: tx}
}

// 5.3: trade + ledger tx + every entry are written in one committed transaction.
func TestJournal_TradeAndEntriesInOneTransaction(t *testing.T) {
	d := &fakeDB{}
	if err := newJournalRepository(d).SaveTradesWithJournal(context.Background(), []ports.TradeWithJournal{sampleTrade(t, "T1"), sampleTrade(t, "T2")}); err != nil {
		t.Fatal(err)
	}
	if len(d.txs) != 1 || !d.txs[0].committed {
		t.Fatalf("want exactly one committed tx, got %d", len(d.txs))
	}
	var trades, txs, entries int
	for _, e := range d.committed {
		switch {
		case strings.Contains(e.sql, "INTO trades"):
			trades++
		case strings.Contains(e.sql, "INTO ledger_transactions"):
			txs++
		case strings.Contains(e.sql, "INTO ledger_entries"):
			entries++
		}
	}
	if trades != 2 || txs != 2 || entries != 10 {
		t.Fatalf("trades=%d txs=%d entries=%d, want 2/2/10", trades, txs, entries)
	}
	if len(d.log) != 0 {
		t.Errorf("%d statements ran outside the transaction", len(d.log))
	}
}

// 5.3: a failure midway (any statement) leaves nothing committed and is returned.
func TestJournal_FailureMidwayWritesNothing(t *testing.T) {
	boom := errors.New("disk full")
	for failAt := 1; failAt <= 6; failAt++ { // trade, tx, entry1.. entry4
		d := &fakeDB{failAt: failAt, txFailErr: boom}
		err := newJournalRepository(d).SaveTradesWithJournal(context.Background(), []ports.TradeWithJournal{sampleTrade(t, "T1")})
		if !errors.Is(err, boom) {
			t.Fatalf("failAt=%d err=%v, want the DB error", failAt, err)
		}
		if len(d.committed) != 0 || d.txs[0].committed || !d.txs[0].rolledBack {
			t.Errorf("failAt=%d: committed=%d rolledBack=%v", failAt, len(d.committed), d.txs[0].rolledBack)
		}
	}
}

// 5.3: an unbalanced voucher is refused before any statement is sent.
func TestJournal_UnbalancedVoucherRefused(t *testing.T) {
	it := sampleTrade(t, "T1")
	it.Journal.Entries[4].Amount++ // fee collector leg off by one
	d := &fakeDB{}
	err := newJournalRepository(d).SaveTradesWithJournal(context.Background(), []ports.TradeWithJournal{it})
	if !errors.Is(err, common.ErrUnbalancedLedger) {
		t.Fatalf("err=%v want ErrUnbalancedLedger", err)
	}
	if len(d.txs) != 0 || d.execs != 0 {
		t.Errorf("began %d txs / %d execs for a bad voucher", len(d.txs), d.execs)
	}
}

func openOrder(t *testing.T) *order.Order {
	t.Helper()
	o, err := order.NewOrder("O1", "C1", "ACC", "APX", order.SideBuy, order.TypeLimit, 19500, 10)
	if err != nil {
		t.Fatal(err)
	}
	o.Reserve(1_952_925)
	o.ApplyFill(4)
	return o
}

// 5.3: Save upserts status, filled quantity, ReservedAmount and RemainingReserved.
func TestOrderRepo_SaveUpsertsAllFields(t *testing.T) {
	d := &fakeDB{}
	o := openOrder(t)
	if err := newOrderRepository(d).Save(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	e := d.log[0]
	for _, want := range []string{"INSERT INTO orders", "ON CONFLICT (order_id) DO UPDATE", "filled_quantity = EXCLUDED.filled_quantity",
		"status = EXCLUDED.status", "remaining_reserved = EXCLUDED.remaining_reserved", "reserved_amount"} {
		if !strings.Contains(e.sql, want) {
			t.Errorf("SQL lacks %q", want)
		}
	}
	if strings.Contains(e.sql, "seq") {
		t.Error("seq must be assigned by the database, not by Save")
	}
	if e.args[7] != int64(10) || e.args[8] != int64(4) || e.args[9] != string(order.StatusPartiallyFilled) ||
		e.args[10] != o.ReservedAmount || e.args[11] != o.RemainingReserved {
		t.Errorf("args=%v", e.args)
	}
}

func TestOrderRepo_SaveErrorIsReturned(t *testing.T) {
	boom := errors.New("conn")
	if err := newOrderRepository(&fakeDB{execErr: boom}).Save(context.Background(), openOrder(t)); !errors.Is(err, boom) {
		t.Fatalf("err=%v", err)
	}
}

func orderRow(id, side, status string, price, qty, filled, reserved, remaining int64, created time.Time) []any {
	return []any{id, "C-" + id, "ACC", "APX", side, "LIMIT", price, qty, filled, status, reserved, remaining, created, created}
}

// 5.4: ListOpen keeps the database order (time priority) and rebuilds orders via order.Restore.
func TestOrderRepo_ListOpenRestoresInPriorityOrder(t *testing.T) {
	now := time.Now()
	d := &fakeDB{rows: [][]any{
		orderRow("O-OLD", "BUY", "NEW", 19500, 100, 0, 1_000, 1_000, now),
		orderRow("O-NEW", "SELL", "PARTIALLY_FILLED", 19600, 50, 20, 0, 0, now),
	}}
	got, err := newOrderRepository(d).ListOpen(context.Background(), "APX")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].ID != "O-OLD" || got[1].ID != "O-NEW" {
		t.Fatalf("order of results: %v", got)
	}
	if got[1].FilledQuantity != 20 || got[1].RemainingQuantity() != 30 || got[0].RemainingReserved != 1_000 {
		t.Errorf("restored %+v / %+v", got[0], got[1])
	}
}

func TestOrderRepo_ListOpenSQL(t *testing.T) {
	var sql string
	d := &sqlSpyDB{fakeDB: &fakeDB{}, onQuery: func(s string) { sql = s }}
	if _, err := newOrderRepository(d).ListOpen(context.Background(), "APX"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "ORDER BY seq ASC") || !strings.Contains(sql, "'NEW', 'PARTIALLY_FILLED'") {
		t.Errorf("ListOpen SQL: %s", sql)
	}
}

type sqlSpyDB struct {
	*fakeDB
	onQuery func(string)
}

func (s *sqlSpyDB) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	s.onQuery(sql)
	return s.fakeDB.Query(ctx, sql, args...)
}

func TestOrderRepo_CorruptRowIsAnError(t *testing.T) {
	d := &fakeDB{rows: [][]any{orderRow("O1", "BUY", "NEW", 19500, 10, 99, 0, 0, time.Now())}} // filled > quantity
	if _, err := newOrderRepository(d).ListOpen(context.Background(), "APX"); !errors.Is(err, common.ErrInvalidState) {
		t.Fatalf("err=%v want ErrInvalidState", err)
	}
}
