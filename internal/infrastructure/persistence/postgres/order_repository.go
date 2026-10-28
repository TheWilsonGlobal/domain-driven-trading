package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresOrderRepository implements ports.OrderRepository on the orders table
// (migration 003 adds the reservation columns and the time-priority sequence).
type PostgresOrderRepository struct {
	pool db
}

// NewPostgresOrderRepository requires a live pool: the in-memory flavour is
// the memory package, chosen explicitly by the composition root.
func NewPostgresOrderRepository(pool *pgxpool.Pool) *PostgresOrderRepository {
	return &PostgresOrderRepository{pool: pool}
}

func newOrderRepository(d db) *PostgresOrderRepository { return &PostgresOrderRepository{pool: d} }

const orderColumns = `order_id, client_order_id, account_id, symbol, side, order_type, price,
	quantity, filled_quantity, status, reserved_amount, remaining_reserved, created_at, updated_at`

// Save inserts the order or updates its mutable fields. seq (time priority) is
// assigned by the database on the first insert only.
func (r *PostgresOrderRepository) Save(ctx context.Context, o *order.Order) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO orders (`+orderColumns+`)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		ON CONFLICT (order_id) DO UPDATE
		SET filled_quantity = EXCLUDED.filled_quantity,
		    status = EXCLUDED.status,
		    remaining_reserved = EXCLUDED.remaining_reserved,
		    updated_at = EXCLUDED.updated_at`,
		o.ID, o.ClientOrderID, o.AccountID, string(o.Symbol), string(o.Side), string(o.Type),
		o.Price.Int64(), int64(o.Quantity), int64(o.FilledQuantity), string(o.Status),
		o.ReservedAmount, o.RemainingReserved, o.CreatedAt, o.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save order %s: %w", o.ID, err)
	}
	return nil
}

// GetByID returns common.ErrOrderNotFound for an unknown id.
func (r *PostgresOrderRepository) GetByID(ctx context.Context, orderID string) (*order.Order, error) {
	var c orderCols
	err := r.pool.QueryRow(ctx, `SELECT `+orderColumns+` FROM orders WHERE order_id = $1`, orderID).Scan(c.targets()...)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, common.ErrOrderNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load order %s: %w", orderID, err)
	}
	return c.restore()
}

// ListOpen returns the NEW / PARTIALLY_FILLED limit orders of a symbol, oldest
// first by seq, which is the book's time priority.
func (r *PostgresOrderRepository) ListOpen(ctx context.Context, symbol order.Symbol) ([]*order.Order, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+orderColumns+` FROM orders
		WHERE symbol = $1 AND order_type = 'LIMIT' AND status IN ('NEW', 'PARTIALLY_FILLED')
		ORDER BY seq ASC`, string(symbol))
	if err != nil {
		return nil, fmt.Errorf("list open orders: %w", err)
	}
	defer rows.Close()
	var out []*order.Order
	for rows.Next() {
		var c orderCols
		if err := rows.Scan(c.targets()...); err != nil {
			return nil, fmt.Errorf("scan order: %w", err)
		}
		o, err := c.restore()
		if err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read open orders: %w", err)
	}
	return out, nil
}

// orderCols is the scan target of one orders row.
type orderCols struct {
	id, clientID, accountID, symbol, side, typ, status string
	price, qty, filled, reserved, remaining            int64
	created, updated                                   time.Time
}

func (c *orderCols) targets() []any {
	return []any{&c.id, &c.clientID, &c.accountID, &c.symbol, &c.side, &c.typ, &c.price,
		&c.qty, &c.filled, &c.status, &c.reserved, &c.remaining, &c.created, &c.updated}
}

// restore builds the domain order through order.Restore, which validates it.
func (c *orderCols) restore() (*order.Order, error) {
	if c.qty < 0 || c.qty > math.MaxUint32 || c.filled < 0 || c.filled > math.MaxUint32 {
		return nil, fmt.Errorf("order %s quantity out of range: %w", c.id, common.ErrInvalidState)
	}
	return order.Restore(order.State{
		ID: c.id, ClientOrderID: c.clientID, AccountID: c.accountID,
		Symbol: order.Symbol(c.symbol), Side: order.OrderSide(c.side), Type: order.OrderType(c.typ),
		Price: order.Price(c.price), Quantity: order.Quantity(c.qty), FilledQuantity: order.Quantity(c.filled),
		Status: order.OrderStatus(c.status), ReservedAmount: c.reserved, RemainingReserved: c.remaining,
		CreatedAt: c.created, UpdatedAt: c.updated,
	})
}
