package postgres

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/vortex-trading/vortex-trading-engine/internal/domain/order"
)

type PostgresTradeRepository struct {
	pool *pgxpool.Pool
}

func NewPostgresTradeRepository(pool *pgxpool.Pool) *PostgresTradeRepository {
	return &PostgresTradeRepository{pool: pool}
}

// SaveBatch writes trades in high-performance batches using pgx.Batch
func (r *PostgresTradeRepository) SaveBatch(ctx context.Context, trades []order.OrderMatchedEvent) error {
	if len(trades) == 0 {
		return nil
	}

	batch := &pgx.Batch{}
	query := `
		INSERT INTO trades (
			trade_id, symbol, maker_order_id, taker_order_id,
			maker_account_id, taker_account_id, taker_side, price, quantity, executed_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (trade_id) DO NOTHING;
	`

	for _, t := range trades {
		batch.Queue(
			query,
			t.TradeID,
			string(t.Symbol),
			t.MakerOrderID,
			t.TakerOrderID,
			t.MakerAccountID,
			t.TakerAccountID,
			string(t.TakerSide),
			t.Price.Int64(),
			int(t.Quantity.Uint32()),
			t.Timestamp,
		)
	}

	br := r.pool.SendBatch(ctx, batch)
	defer br.Close()

	for range trades {
		if _, err := br.Exec(); err != nil {
			return err
		}
	}

	return nil
}
