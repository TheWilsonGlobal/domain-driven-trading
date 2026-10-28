package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// db is the subset of *pgxpool.Pool the repository uses (and tests fake).
type db interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

type PostgresAccountRepository struct {
	pool       db // nil: in-memory only
	inMemoryMu sync.RWMutex
	memoryAccs map[string]*account.Account
}

// NewPostgresAccountRepository returns a repository backed by pool; a nil pool
// keeps accounts in memory only. It owns no business data: accounts exist only
// if the seeding step (or a previous Save) created them.
func NewPostgresAccountRepository(pool *pgxpool.Pool) *PostgresAccountRepository {
	if pool == nil {
		return newRepository(nil)
	}
	return newRepository(pool)
}

func newRepository(d db) *PostgresAccountRepository {
	return &PostgresAccountRepository{pool: d, memoryAccs: make(map[string]*account.Account)}
}

// GetByID returns the account, or common.ErrAccountNotFound when it does not
// exist. It never creates an account. Any other storage failure is returned
// as-is so callers cannot mistake an outage for a missing account.
func (r *PostgresAccountRepository) GetByID(ctx context.Context, accountID string) (*account.Account, error) {
	r.inMemoryMu.RLock()
	acc, exists := r.memoryAccs[accountID]
	r.inMemoryMu.RUnlock()
	if exists {
		return acc, nil
	}
	if r.pool == nil {
		return nil, common.ErrAccountNotFound
	}

	var cashBalance, lockedCash int64
	err := r.pool.QueryRow(ctx, "SELECT cash_balance, locked_cash FROM accounts WHERE account_id = $1", accountID).Scan(&cashBalance, &lockedCash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, common.ErrAccountNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load account %s: %w", accountID, err)
	}

	rows, err := r.pool.Query(ctx, "SELECT symbol, quantity, locked_quantity FROM account_holdings WHERE account_id = $1", accountID)
	if err != nil {
		return nil, fmt.Errorf("load holdings %s: %w", accountID, err)
	}
	defer rows.Close()
	holdings := map[order.Symbol]uint32{}
	lockedStocks := map[order.Symbol]uint32{}
	for rows.Next() {
		var sym string
		var qty, lockedQty int64
		if err := rows.Scan(&sym, &qty, &lockedQty); err != nil {
			return nil, fmt.Errorf("scan holdings %s: %w", accountID, err)
		}
		if qty < 0 || qty > math.MaxUint32 || lockedQty < 0 || lockedQty > math.MaxUint32 {
			return nil, fmt.Errorf("holding %s/%s out of range: %w", accountID, sym, common.ErrInvalidState)
		}
		holdings[order.Symbol(sym)] = uint32(qty)
		if lockedQty > 0 {
			lockedStocks[order.Symbol(sym)] = uint32(lockedQty)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read holdings %s: %w", accountID, err)
	}
	// The domain factory validates the loaded state; the adapter never writes
	// LockedCash / LockedStocks itself.
	loadedAcc, err := account.Restore(accountID, cashBalance, lockedCash, holdings, lockedStocks, time.Now())
	if err != nil {
		return nil, fmt.Errorf("restore account %s: %w", accountID, err)
	}

	r.inMemoryMu.Lock()
	defer r.inMemoryMu.Unlock()
	if existing, ok := r.memoryAccs[accountID]; ok { // lost a load race: keep the live one
		return existing, nil
	}
	r.memoryAccs[accountID] = loadedAcc
	return loadedAcc, nil
}

// Save persists cash, locks and every holding (quantity and locked quantity)
// in one transaction, then makes the cache reflect it. The store is written
// first: a failed write returns the error and leaves the cache as it was.
// When acc is not the cached object its state is copied INTO the cached one
// (never swapped), so goroutines holding the old pointer keep seeing live state.
func (r *PostgresAccountRepository) Save(ctx context.Context, acc *account.Account) error {
	snap := acc.Snapshot()
	if r.pool != nil {
		if err := r.writeAccount(ctx, snap); err != nil {
			return fmt.Errorf("save account %s: %w", snap.ID, err)
		}
	}

	r.inMemoryMu.Lock()
	defer r.inMemoryMu.Unlock()
	cached, ok := r.memoryAccs[snap.ID]
	switch {
	case !ok:
		r.memoryAccs[snap.ID] = acc
	case cached != acc:
		return cached.ReplaceWith(snap)
	}
	return nil
}

func (r *PostgresAccountRepository) writeAccount(ctx context.Context, snap *account.Account) (err error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	if _, err = tx.Exec(ctx, `
		INSERT INTO accounts (account_id, cash_balance, locked_cash, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (account_id) DO UPDATE
		SET cash_balance = EXCLUDED.cash_balance,
		    locked_cash = EXCLUDED.locked_cash,
		    updated_at = NOW()
	`, snap.ID, snap.CashBalance, snap.LockedCash); err != nil {
		return err
	}

	symbols := map[order.Symbol]struct{}{}
	for sym := range snap.Holdings {
		symbols[sym] = struct{}{}
	}
	for sym := range snap.LockedStocks {
		symbols[sym] = struct{}{}
	}
	for sym := range symbols {
		if _, err = tx.Exec(ctx, `
			INSERT INTO account_holdings (account_id, symbol, quantity, locked_quantity, updated_at)
			VALUES ($1, $2, $3, $4, NOW())
			ON CONFLICT (account_id, symbol) DO UPDATE
			SET quantity = EXCLUDED.quantity,
			    locked_quantity = EXCLUDED.locked_quantity,
			    updated_at = NOW()
		`, snap.ID, string(sym), int64(snap.Holdings[sym]), int64(snap.LockedStocks[sym])); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// reserveCashSQL is the atomic conditional reserve: the row is updated only if
// the free cash still covers the amount, so concurrent writers can never
// over-reserve.
const reserveCashSQL = `UPDATE accounts SET locked_cash = locked_cash + $1, updated_at = NOW()
	WHERE account_id = $2 AND cash_balance - locked_cash >= $1`

// ReserveCash implements ports.CashReserver. Zero affected rows means the
// account has too little free cash (or does not exist) and yields
// common.ErrInsufficientBalance. Without a pool there is nothing to persist.
func (r *PostgresAccountRepository) ReserveCash(ctx context.Context, accountID string, amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("reserve %d: %w", amount, common.ErrInvalidState)
	}
	if r.pool == nil {
		return nil
	}
	tag, err := r.pool.Exec(ctx, reserveCashSQL, amount, accountID)
	if err != nil {
		return fmt.Errorf("reserve cash %s: %w", accountID, err)
	}
	if tag.RowsAffected() == 0 {
		return common.ErrInsufficientBalance
	}
	return nil
}
