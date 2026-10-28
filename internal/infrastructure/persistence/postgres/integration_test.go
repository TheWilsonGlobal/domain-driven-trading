package postgres_test

// Integration test against a REAL PostgreSQL. It is skipped unless
// DDT_TEST_DATABASE_URL is set (point it at a local/throwaway server, never at
// production or Supabase). Everything happens inside a throwaway schema that is
// created for the run and dropped at the end: migrations 001 + 003 are applied
// there, so no shared table is touched.

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/seed"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/persistence/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func openPool(t *testing.T, url, schema string) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal("DDT_TEST_DATABASE_URL is not a valid connection string")
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	cfg.MaxConns = 4
	if schema != "" {
		cfg.ConnConfig.RuntimeParams["search_path"] = schema
	}
	p, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("open pool: %v", err)
	}
	return p
}

func TestIntegration_PersistenceAndRecovery(t *testing.T) {
	url := os.Getenv("DDT_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("DDT_TEST_DATABASE_URL not set: skipping the PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	schema := fmt.Sprintf("ddt_itest_%d", rand.Int63())
	admin := openPool(t, url, "")
	defer admin.Close()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	defer func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") }()

	pool := openPool(t, url, schema)
	defer pool.Close()
	for _, f := range []string{"001_init.sql", "003_persistence_recovery.sql"} {
		sql, err := os.ReadFile("../migrations/" + f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", f, err)
		}
	}

	const sym = order.Symbol("APX")
	boot := func(p *pgxpool.Pool) (*order.Matcher, *postgres.PostgresAccountRepository, *postgres.PostgresOrderRepository, *command.PlaceOrderHandler, *command.CancelOrderHandler) {
		accounts := postgres.NewPostgresAccountRepository(p)
		orders := postgres.NewPostgresOrderRepository(p)
		m := order.NewMatcher(order.NewOrderBook(sym))
		place := command.NewPlaceOrderHandler(m, accounts, nil).WithOrderStore(orders, postgres.NewPostgresTradeJournalRepository(p))
		cancel := command.NewCancelOrderHandler(m, accounts, nil).WithOrderStore(orders)
		return m, accounts, orders, place, cancel
	}

	m, accounts, _, place, _ := boot(pool)
	list := []seed.Account{
		{ID: "IT_BUYER", Cash: 1_000_000_000},
		{ID: "IT_SELLER", Cash: 0, Holdings: map[order.Symbol]uint32{sym: 1000}},
	}
	if _, err := seed.Accounts(ctx, accounts, list, nil); err != nil {
		t.Fatalf("seed: %v", err)
	}
	// 5.2: a fresh repository (restart) sees the seeded holdings
	if got, err := postgres.NewPostgresAccountRepository(pool).GetByID(ctx, "IT_SELLER"); err != nil || got.Snapshot().Holdings[sym] != 1000 {
		t.Fatalf("seeded holdings after reload: %+v err=%v", got, err)
	}

	do := func(acc string, side order.OrderSide, price order.Price, qty order.Quantity) *command.PlaceOrderResult {
		t.Helper()
		res, err := place.Handle(ctx, command.PlaceOrderCommand{AccountID: acc, Symbol: sym, Side: side, Type: order.TypeLimit, Price: price, Quantity: qty})
		if err != nil {
			t.Fatalf("place: %v", err)
		}
		return res
	}
	buy := do("IT_BUYER", order.SideBuy, 19500, 100)
	do("IT_BUYER", order.SideBuy, 19400, 50)
	sell := do("IT_SELLER", order.SideSell, 19500, 40) // fills 40 of the first bid
	do("IT_SELLER", order.SideSell, 20000, 200)        // rests, locks shares
	if len(sell.Matches) != 1 {
		t.Fatalf("matches = %d", len(sell.Matches))
	}

	// 5.1: the conditional reserve refuses more than the free cash, on the real table
	if err := accounts.ReserveCash(ctx, "IT_BUYER", 1<<60); err != common.ErrInsufficientBalance {
		t.Fatalf("ReserveCash over the limit: %v, want ErrInsufficientBalance", err)
	}

	// 5.3: trade + journal rows exist and every transaction balances per asset
	var trades, txs int
	if err := pool.QueryRow(ctx, "SELECT (SELECT count(*) FROM trades), (SELECT count(*) FROM ledger_transactions)").Scan(&trades, &txs); err != nil {
		t.Fatal(err)
	}
	if trades != 1 || txs != 1 {
		t.Fatalf("trades=%d ledger_transactions=%d, want 1/1", trades, txs)
	}
	var unbalanced int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM (
			SELECT tx_id, asset_type FROM ledger_entries GROUP BY tx_id, asset_type
			HAVING SUM(CASE WHEN entry_type = 'DEBIT' THEN amount ELSE -amount END) <> 0) x`).Scan(&unbalanced); err != nil {
		t.Fatal(err)
	}
	if unbalanced != 0 {
		t.Fatalf("%d unbalanced (tx, asset) groups", unbalanced)
	}
	var feeCash int64
	if err := pool.QueryRow(ctx, "SELECT cash_balance FROM accounts WHERE account_id = $1", account.FeeCollectorID).Scan(&feeCash); err != nil {
		t.Fatal(err)
	}
	fee, _ := account.Fee(19500, 40)
	if feeCash != 2*fee {
		t.Fatalf("FEE_COLLECTOR cash=%d want %d", feeCash, 2*fee)
	}

	// snapshot the live state, then "restart": new pool, new repos, empty book
	wantDepth := m.Depth(50)
	wantLocks := map[string][3]int64{}
	for _, id := range []string{"IT_BUYER", "IT_SELLER"} {
		s := mustGet(t, accounts, id)
		wantLocks[id] = [3]int64{s.CashBalance, s.LockedCash, int64(s.LockedStocks[sym])}
	}
	pool.Close()

	pool2 := openPool(t, url, schema)
	defer pool2.Close()
	m2, accounts2, _, place2, cancel2 := boot(pool2)
	rep, err := command.RecoverBook(ctx, m2, postgres.NewPostgresOrderRepository(pool2), accounts2, nil)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if rep.Restored != 3 || rep.LocksCorrected != 0 {
		t.Fatalf("recovery report %+v, want 3 restored and 0 corrections", rep)
	}
	gotDepth := m2.Depth(50)
	if !reflect.DeepEqual(gotDepth.Bids, wantDepth.Bids) || !reflect.DeepEqual(gotDepth.Asks, wantDepth.Asks) {
		t.Fatalf("depth after restart\n got %+v / %+v\nwant %+v / %+v", gotDepth.Bids, gotDepth.Asks, wantDepth.Bids, wantDepth.Asks)
	}
	for id, want := range wantLocks {
		s := mustGet(t, accounts2, id)
		if got := [3]int64{s.CashBalance, s.LockedCash, int64(s.LockedStocks[sym])}; got != want {
			t.Errorf("%s cash/lockedCash/lockedStocks after restart %v, want %v", id, got, want)
		}
	}

	// the recovered engine keeps trading: the older bid (partially filled) fills first
	res, err := place2.Handle(ctx, command.PlaceOrderCommand{AccountID: "IT_SELLER", Symbol: sym, Side: order.SideSell, Type: order.TypeLimit, Price: 19500, Quantity: 10})
	if err != nil || len(res.Matches) != 1 || res.Matches[0].MakerOrderID != buy.Order.ID {
		t.Fatalf("post-restart trade: %+v err=%v", res, err)
	}
	if _, err := cancel2.Handle(ctx, command.CancelOrderCommand{OrderID: buy.Order.ID, AccountID: "IT_BUYER", Symbol: sym}); err != nil {
		t.Fatalf("cancel restored order: %v", err)
	}
}

func mustGet(t *testing.T, r *postgres.PostgresAccountRepository, id string) *account.Account {
	t.Helper()
	a, err := r.GetByID(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return a.Snapshot()
}

// F4: migration 003 adds orders.seq to a table that already holds rows. The
// backfill must follow created_at (then order_id), not physical row order, so
// existing resting orders keep their time priority.
func TestIntegration_Migration003BackfillsSeqInCreatedAtOrder(t *testing.T) {
	url := os.Getenv("DDT_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("DDT_TEST_DATABASE_URL not set: skipping the PostgreSQL integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	schema := fmt.Sprintf("ddt_itest_%d", rand.Int63())
	admin := openPool(t, url, "")
	defer admin.Close()
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	defer func() { _, _ = admin.Exec(context.Background(), "DROP SCHEMA "+schema+" CASCADE") }()
	pool := openPool(t, url, schema)
	defer pool.Close()

	apply := func(f string) {
		t.Helper()
		sql, err := os.ReadFile("../migrations/" + f)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", f, err)
		}
	}
	apply("001_init.sql")
	if _, err := pool.Exec(ctx, "INSERT INTO accounts (account_id) VALUES ('A1')"); err != nil {
		t.Fatal(err)
	}
	// Inserted newest-first, with order ids that sort differently from created_at.
	ids := []string{"o_c", "o_a", "o_d", "o_b"}
	for i, id := range ids {
		if _, err := pool.Exec(ctx, `INSERT INTO orders (order_id, client_order_id, account_id, symbol, side, order_type, price, quantity, status, created_at)
			VALUES ($1, $1, 'A1', 'APX', 'BUY', 'LIMIT', 19500, 10, 'NEW', now() - ($2 || ' minutes')::interval)`, id, fmt.Sprint(i+1)); err != nil {
			t.Fatal(err)
		}
	}
	// created_at order (oldest first) is the reverse of insertion: o_b, o_d, o_a, o_c.
	want := []string{"o_b", "o_d", "o_a", "o_c"}

	order3 := func() []string {
		t.Helper()
		rows, err := pool.Query(ctx, "SELECT order_id FROM orders ORDER BY seq")
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		var got []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			got = append(got, id)
		}
		return got
	}

	apply("003_persistence_recovery.sql")
	if got := order3(); !reflect.DeepEqual(got, want) {
		t.Fatalf("seq order after backfill = %v, want created_at order %v", got, want)
	}
	apply("003_persistence_recovery.sql") // idempotent, must not renumber
	if got := order3(); !reflect.DeepEqual(got, want) {
		t.Fatalf("seq order after re-apply = %v, want %v", got, want)
	}
	// New rows continue after the backfilled ones and are NOT NULL.
	if _, err := pool.Exec(ctx, `INSERT INTO orders (order_id, client_order_id, account_id, symbol, side, order_type, price, quantity, status)
		VALUES ('o_new', 'o_new', 'A1', 'APX', 'BUY', 'LIMIT', 19500, 10, 'NEW')`); err != nil {
		t.Fatal(err)
	}
	if got := order3(); got[len(got)-1] != "o_new" {
		t.Fatalf("new order did not get the highest seq: %v", got)
	}
	var nullable string
	if err := pool.QueryRow(ctx, `SELECT is_nullable FROM information_schema.columns WHERE table_schema = $1 AND table_name = 'orders' AND column_name = 'seq'`, schema).Scan(&nullable); err != nil || nullable != "NO" {
		t.Fatalf("seq is_nullable = %q err=%v, want NO", nullable, err)
	}
}
