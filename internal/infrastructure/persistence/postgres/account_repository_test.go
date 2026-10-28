package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type fakeRow struct {
	err        error
	cash, lock int64
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	*(dest[0].(*int64)) = r.cash
	*(dest[1].(*int64)) = r.lock
	return nil
}

type emptyRows struct{ pgx.Rows }

func (emptyRows) Next() bool { return false }
func (emptyRows) Close()     {}
func (emptyRows) Err() error { return nil }

type execRec struct {
	sql  string
	args []any
}

// fakeDB fakes the db interface. Statements run through a transaction are
// logged on the transaction and reach `committed` only on Commit, so tests can
// assert "all rows or none".
type fakeDB struct {
	row       fakeRow
	rows      [][]any // rows returned by Query (holdings, orders, ...)
	execs     int
	log       []execRec // direct (non-transactional) Exec calls
	tag       string    // command tag returned by direct Exec, default "UPDATE 1"
	execErr   error     // direct Exec error
	beginErr  error
	failAt    int // 1-based index of the in-transaction Exec that fails (0: none)
	txFailErr error
	committed []execRec
	txs       []*fakeTx
}

type fakeTx struct {
	pgx.Tx
	db         *fakeDB
	log        []execRec
	committed  bool
	rolledBack bool
}

func (f *fakeDB) QueryRow(context.Context, string, ...any) pgx.Row { return f.row }
func (f *fakeDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	if len(f.rows) == 0 {
		return emptyRows{}, nil
	}
	return &sliceRows{rows: f.rows}, nil
}
func (f *fakeDB) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	f.execs++
	f.log = append(f.log, execRec{sql, args})
	tag := f.tag
	if tag == "" {
		tag = "UPDATE 1"
	}
	return pgconn.NewCommandTag(tag), f.execErr
}
func (f *fakeDB) Begin(context.Context) (pgx.Tx, error) {
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	tx := &fakeTx{db: f}
	f.txs = append(f.txs, tx)
	return tx, nil
}

func (t *fakeTx) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	t.db.execs++
	t.log = append(t.log, execRec{sql, args})
	if t.db.failAt > 0 && len(t.log) == t.db.failAt {
		return pgconn.CommandTag{}, t.db.txFailErr
	}
	return pgconn.NewCommandTag("INSERT 0 1"), nil
}
func (t *fakeTx) Commit(context.Context) error {
	t.committed = true
	t.db.committed = append(t.db.committed, t.log...)
	return nil
}
func (t *fakeTx) Rollback(context.Context) error {
	if !t.committed {
		t.rolledBack = true
	}
	return nil
}

// sliceRows serves canned rows, assigning each column into the Scan targets.
type sliceRows struct {
	pgx.Rows
	rows [][]any
	i    int
}

func (r *sliceRows) Next() bool             { r.i++; return r.i <= len(r.rows) }
func (r *sliceRows) Close()                 {}
func (r *sliceRows) Err() error             { return nil }
func (r *sliceRows) Scan(dest ...any) error { return assign(r.rows[r.i-1], dest) }

func assign(vals []any, dest []any) error {
	if len(vals) != len(dest) {
		return fmt.Errorf("scan: %d columns into %d targets", len(vals), len(dest))
	}
	for i, d := range dest {
		switch p := d.(type) {
		case *string:
			*p = vals[i].(string)
		case *int64:
			*p = vals[i].(int64)
		case *time.Time:
			*p = vals[i].(time.Time)
		default:
			return fmt.Errorf("unsupported scan target %T", d)
		}
	}
	return nil
}

func TestGetByID_NoRowsIsNotFoundAndInsertsNothing(t *testing.T) {
	db := &fakeDB{row: fakeRow{err: pgx.ErrNoRows}}
	repo := newRepository(db)
	acc, err := repo.GetByID(context.Background(), "NOPE")
	if acc != nil || !errors.Is(err, common.ErrAccountNotFound) {
		t.Fatalf("acc=%v err=%v, want nil + ErrAccountNotFound", acc, err)
	}
	if db.execs != 0 {
		t.Fatalf("read path executed %d writes, want 0", db.execs)
	}
	if _, cached := repo.memoryAccs["NOPE"]; cached {
		t.Fatal("unknown account was cached")
	}
}

func TestGetByID_DBErrorIsNotNotFoundAndNeverResets(t *testing.T) {
	boom := errors.New("connection reset")
	db := &fakeDB{row: fakeRow{err: boom}}
	repo := newRepository(db)
	acc, err := repo.GetByID(context.Background(), "ACC_X")
	if acc != nil || !errors.Is(err, boom) || errors.Is(err, common.ErrAccountNotFound) {
		t.Fatalf("acc=%v err=%v, want the DB error passed through", acc, err)
	}
	if db.execs != 0 {
		t.Fatalf("executed %d writes after a DB error, want 0", db.execs)
	}
	if _, cached := repo.memoryAccs["ACC_X"]; cached {
		t.Fatal("failed load was cached")
	}
}

func TestGetByID_LoadsExistingAccount(t *testing.T) {
	db := &fakeDB{row: fakeRow{cash: 123, lock: 5}}
	acc, err := newRepository(db).GetByID(context.Background(), "ACC_X")
	if err != nil || acc.CashBalance != 123 || acc.LockedCash != 5 {
		t.Fatalf("acc=%+v err=%v", acc, err)
	}
}

func TestGetByID_WithoutPoolUnknownIsNotFound(t *testing.T) {
	_, err := NewPostgresAccountRepository(nil).GetByID(context.Background(), "NOPE")
	if !errors.Is(err, common.ErrAccountNotFound) {
		t.Fatalf("err=%v", err)
	}
}

// Plan 4.4: the adapter owns no business data.
func TestAdapterSourceHasNoStarterData(t *testing.T) {
	src, err := os.ReadFile("account_repository.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, banned := range []string{"ACC_APX", "ACC_INST", "FEE_COLLECTOR", "500_000_000", "100_000_000", "20_000_000_000", "seedInitialAccounts"} {
		if strings.Contains(string(src), banned) {
			t.Errorf("account_repository.go still contains %q", banned)
		}
	}
}
