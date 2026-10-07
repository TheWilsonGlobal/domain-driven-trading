package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vortex-trading/vortex-trading-engine/internal/domain/account"
)

type PostgresLedgerRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresLedgerRepository(pool *pgxpool.Pool) *PostgresLedgerRepository {
	return &PostgresLedgerRepository{pool: pool}
}

func (r *PostgresLedgerRepository) SaveTransaction(ctx context.Context, tx *account.Transaction) error {
	pgxTx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer pgxTx.Rollback(ctx)

	_, err = pgxTx.Exec(ctx, `
		INSERT INTO ledger_transactions (tx_id, trade_id, created_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (tx_id) DO NOTHING;
	`, tx.ID, tx.TradeID, tx.Timestamp)
	if err != nil {
		return err
	}

	entryQuery := `
		INSERT INTO ledger_entries (
			entry_id, tx_id, account_id, entry_type, asset_type, symbol, amount, description, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (entry_id) DO NOTHING;
	`
	for _, entry := range tx.Entries {
		_, err = pgxTx.Exec(ctx, entryQuery,
			entry.ID,
			entry.TxID,
			entry.AccountID,
			string(entry.Type),
			string(entry.AssetType),
			entry.Symbol,
			entry.Amount,
			entry.Description,
			entry.Timestamp,
		)
		if err != nil {
			return err
		}
	}

	return pgxTx.Commit(ctx)
}
