package postgres

import (
	"context"
	"fmt"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresTradeJournalRepository implements ports.TradeJournalRepository: trades,
// ledger transactions and ledger entries of a command are written in ONE
// database transaction. Inserts are idempotent (ON CONFLICT DO NOTHING), so a
// replay after a crash is harmless.
type PostgresTradeJournalRepository struct {
	pool db
}

func NewPostgresTradeJournalRepository(pool *pgxpool.Pool) *PostgresTradeJournalRepository {
	return &PostgresTradeJournalRepository{pool: pool}
}

func newJournalRepository(d db) *PostgresTradeJournalRepository {
	return &PostgresTradeJournalRepository{pool: d}
}

const (
	insertTradeSQL = `
		INSERT INTO trades (
			trade_id, symbol, maker_order_id, taker_order_id,
			maker_account_id, taker_account_id, taker_side, price, quantity, executed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (trade_id) DO NOTHING`
	insertTxSQL = `
		INSERT INTO ledger_transactions (tx_id, trade_id, created_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (tx_id) DO NOTHING`
	insertEntrySQL = `
		INSERT INTO ledger_entries (
			entry_id, tx_id, account_id, entry_type, asset_type, symbol, amount, description, created_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (entry_id) DO NOTHING`
)

// SaveTradesWithJournal writes every item or none of them. A voucher whose
// debits and credits differ per asset is refused before anything is sent.
func (r *PostgresTradeJournalRepository) SaveTradesWithJournal(ctx context.Context, items []ports.TradeWithJournal) (err error) {
	if len(items) == 0 {
		return nil
	}
	for _, it := range items {
		if err := checkBalanced(it.Journal); err != nil {
			return fmt.Errorf("trade %s: %w", it.Trade.TradeID, err)
		}
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin trade+journal tx: %w", err)
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback(ctx)
		}
	}()

	for _, it := range items {
		t := it.Trade
		if _, err = tx.Exec(ctx, insertTradeSQL,
			t.TradeID, string(t.Symbol), t.MakerOrderID, t.TakerOrderID, t.MakerAccountID, t.TakerAccountID,
			string(t.TakerSide), t.Price.Int64(), int(t.Quantity.Uint32()), t.Timestamp); err != nil {
			return fmt.Errorf("insert trade %s: %w", t.TradeID, err)
		}
		j := it.Journal
		if _, err = tx.Exec(ctx, insertTxSQL, j.ID, j.TradeID, j.Timestamp); err != nil {
			return fmt.Errorf("insert ledger tx %s: %w", j.ID, err)
		}
		for _, e := range j.Entries {
			if _, err = tx.Exec(ctx, insertEntrySQL, e.ID, e.TxID, e.AccountID, string(e.Type),
				string(e.AssetType), e.Symbol, e.Amount, e.Description, e.Timestamp); err != nil {
				return fmt.Errorf("insert ledger entry %s: %w", e.ID, err)
			}
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit trade+journal tx: %w", err)
	}
	return nil
}

// checkBalanced verifies Sum(debit) == Sum(credit) for each asset of a voucher.
func checkBalanced(tx *account.Transaction) error {
	if tx == nil || len(tx.Entries) == 0 {
		return common.ErrUnbalancedLedger
	}
	sums := map[account.AssetType]int64{}
	for _, e := range tx.Entries {
		if e.Amount <= 0 {
			return common.ErrNegativeLedgerAmount
		}
		if e.Type == account.Debit {
			sums[e.AssetType] += e.Amount
		} else {
			sums[e.AssetType] -= e.Amount
		}
	}
	for _, net := range sums {
		if net != 0 {
			return common.ErrUnbalancedLedger
		}
	}
	return nil
}
