-- ==============================================================================
-- 003_persistence_recovery.sql: order persistence and crash recovery (plan 5.3/5.4)
-- Additive and idempotent: safe on top of 001 or 002, safe to run twice.
-- ==============================================================================

-- Reservation state of an order. reserved_amount is what was locked on the
-- account at placement (BUY orders, VND); remaining_reserved is the part still
-- locked. Recovery sums remaining_reserved of open BUY orders to rebuild locked_cash.
ALTER TABLE orders ADD COLUMN IF NOT EXISTS reserved_amount BIGINT NOT NULL DEFAULT 0;
ALTER TABLE orders ADD COLUMN IF NOT EXISTS remaining_reserved BIGINT NOT NULL DEFAULT 0;

-- Time priority: assigned once by the database on first insert (never updated
-- by the upsert), so ORDER BY seq reproduces FIFO within a price level.
-- Existing rows are numbered in created_at order (ties: order_id), NOT in
-- physical row order as a plain ADD COLUMN ... BIGSERIAL would do. The block is
-- idempotent: once seq exists it does nothing.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'orders' AND column_name = 'seq'
    ) THEN
        ALTER TABLE orders ADD COLUMN seq BIGINT;
        UPDATE orders o SET seq = r.rn
        FROM (SELECT order_id, ROW_NUMBER() OVER (ORDER BY created_at, order_id) AS rn FROM orders) r
        WHERE o.order_id = r.order_id;
        CREATE SEQUENCE orders_seq_seq OWNED BY orders.seq;
        PERFORM setval('orders_seq_seq', COALESCE((SELECT MAX(seq) FROM orders), 0) + 1, false);
        ALTER TABLE orders ALTER COLUMN seq SET DEFAULT nextval('orders_seq_seq');
        ALTER TABLE orders ALTER COLUMN seq SET NOT NULL;
    END IF;
END $$;
CREATE INDEX IF NOT EXISTS idx_orders_open_seq ON orders(symbol, seq)
    WHERE status IN ('NEW', 'PARTIALLY_FILLED');

-- A MARKET SELL order carries no price (0); the original check (price > 0) in
-- 002 would reject it. Replace it with price >= 0. Limit prices are validated
-- by the application (tick size, 0 < price <= MaxPrice).
ALTER TABLE orders DROP CONSTRAINT IF EXISTS orders_price_check;
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_constraint WHERE conname = 'orders_price_nonneg') THEN
        ALTER TABLE orders ADD CONSTRAINT orders_price_nonneg CHECK (price >= 0);
    END IF;
END $$;

-- One ledger voucher per trade: a replayed write is a no-op, never a duplicate.
CREATE UNIQUE INDEX IF NOT EXISTS uq_ledger_transactions_trade ON ledger_transactions(trade_id);
