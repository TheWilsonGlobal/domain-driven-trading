package postgres

import (
	"context"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

type PostgresAccountRepository struct {
	pool       *pgxpool.Pool
	inMemoryMu sync.RWMutex
	memoryAccs map[string]*account.Account
}

func NewPostgresAccountRepository(pool *pgxpool.Pool) *PostgresAccountRepository {
	repo := &PostgresAccountRepository{
		pool:       pool,
		memoryAccs: make(map[string]*account.Account),
	}
	repo.seedInitialAccounts()
	return repo
}

func (r *PostgresAccountRepository) seedInitialAccounts() {
	// Initialize high-speed in-memory cached accounts
	a1 := account.NewAccount("ACC_APX_001", 500_000_000) // 500M VND
	a1.CreditStock(order.Symbol("APX"), 10000)
	a1.CreditStock(order.Symbol("HPG"), 5000)
	r.memoryAccs["ACC_APX_001"] = a1

	a2 := account.NewAccount("ACC_APX_002", 300_000_000) // 300M VND
	a2.CreditStock(order.Symbol("APX"), 8000)
	r.memoryAccs["ACC_APX_002"] = a2

	mm := account.NewAccount("ACC_INST_MM", 20_000_000_000) // 20B VND
	mm.CreditStock(order.Symbol("APX"), 100000)
	mm.CreditStock(order.Symbol("HPG"), 100000)
	mm.CreditStock(order.Symbol("FPT"), 50000)
	r.memoryAccs["ACC_INST_MM"] = mm

	collector := account.NewAccount("FEE_COLLECTOR", 0)
	r.memoryAccs["FEE_COLLECTOR"] = collector
}

func (r *PostgresAccountRepository) GetByID(ctx context.Context, accountID string) (*account.Account, error) {
	r.inMemoryMu.RLock()
	acc, exists := r.memoryAccs[accountID]
	r.inMemoryMu.RUnlock()

	if exists {
		return acc, nil
	}

	// Query from PostgreSQL / Supabase if pool is provided
	if r.pool != nil {
		var cashBalance, lockedCash int64
		err := r.pool.QueryRow(ctx, "SELECT cash_balance, locked_cash FROM accounts WHERE account_id = $1", accountID).Scan(&cashBalance, &lockedCash)
		if err == nil {
			loadedAcc := account.NewAccount(accountID, cashBalance)
			loadedAcc.LockedCash = lockedCash

			rows, err := r.pool.Query(ctx, "SELECT symbol, quantity, locked_quantity FROM account_holdings WHERE account_id = $1", accountID)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var sym string
					var qty, lockedQty int64
					if err := rows.Scan(&sym, &qty, &lockedQty); err == nil {
						loadedAcc.CreditStock(order.Symbol(sym), uint32(qty))
						loadedAcc.LockedStocks[order.Symbol(sym)] = uint32(lockedQty)
					}
				}
			}

			r.inMemoryMu.Lock()
			r.memoryAccs[accountID] = loadedAcc
			r.inMemoryMu.Unlock()
			return loadedAcc, nil
		}
	}

	// Create and register new account dynamically with standard starter balance
	newAcc := account.NewAccount(accountID, 100_000_000)
	newAcc.CreditStock(order.Symbol("APX"), 2000)
	newAcc.CreditStock(order.Symbol("HPG"), 2000)

	r.inMemoryMu.Lock()
	r.memoryAccs[accountID] = newAcc
	r.inMemoryMu.Unlock()

	if r.pool != nil {
		_, _ = r.pool.Exec(ctx, `
			INSERT INTO accounts (account_id, cash_balance, locked_cash, updated_at)
			VALUES ($1, $2, $3, NOW())
			ON CONFLICT (account_id) DO NOTHING;
		`, newAcc.ID, newAcc.CashBalance, newAcc.LockedCash)
	}

	return newAcc, nil
}

func (r *PostgresAccountRepository) Save(ctx context.Context, acc *account.Account) error {
	r.inMemoryMu.Lock()
	r.memoryAccs[acc.ID] = acc
	r.inMemoryMu.Unlock()

	if r.pool != nil {
		_, _ = r.pool.Exec(ctx, `
			INSERT INTO accounts (account_id, cash_balance, locked_cash, updated_at)
			VALUES ($1, $2, $3, NOW())
			ON CONFLICT (account_id) DO UPDATE
			SET cash_balance = EXCLUDED.cash_balance,
			    locked_cash = EXCLUDED.locked_cash,
			    updated_at = NOW();
		`, acc.ID, acc.CashBalance, acc.LockedCash)
	}

	return nil
}
