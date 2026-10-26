-- ==============================================================================
-- 002_supabase_init.sql: Supabase PostgreSQL Schema & Security Hardening
-- ==============================================================================

-- 1. Create Core Tables
CREATE TABLE IF NOT EXISTS accounts (
    account_id VARCHAR(64) PRIMARY KEY,
    cash_balance BIGINT NOT NULL DEFAULT 0 CHECK (cash_balance >= 0),
    locked_cash BIGINT NOT NULL DEFAULT 0 CHECK (locked_cash >= 0),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS account_holdings (
    account_id VARCHAR(64) NOT NULL REFERENCES accounts(account_id) ON DELETE CASCADE,
    symbol VARCHAR(16) NOT NULL,
    quantity BIGINT NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    locked_quantity BIGINT NOT NULL DEFAULT 0 CHECK (locked_quantity >= 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (account_id, symbol)
);

CREATE TABLE IF NOT EXISTS orders (
    order_id VARCHAR(64) PRIMARY KEY,
    client_order_id VARCHAR(128) NOT NULL,
    account_id VARCHAR(64) NOT NULL REFERENCES accounts(account_id),
    symbol VARCHAR(16) NOT NULL,
    side VARCHAR(8) NOT NULL,
    order_type VARCHAR(16) NOT NULL,
    price BIGINT NOT NULL CHECK (price > 0),
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    filled_quantity INTEGER NOT NULL DEFAULT 0 CHECK (filled_quantity >= 0),
    status VARCHAR(32) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_orders_account ON orders(account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_orders_symbol_status ON orders(symbol, status);

CREATE TABLE IF NOT EXISTS trades (
    trade_id VARCHAR(64) PRIMARY KEY,
    symbol VARCHAR(16) NOT NULL,
    maker_order_id VARCHAR(64) NOT NULL,
    taker_order_id VARCHAR(64) NOT NULL,
    maker_account_id VARCHAR(64) NOT NULL REFERENCES accounts(account_id),
    taker_account_id VARCHAR(64) NOT NULL REFERENCES accounts(account_id),
    taker_side VARCHAR(8) NOT NULL,
    price BIGINT NOT NULL CHECK (price > 0),
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    executed_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_trades_symbol_time ON trades(symbol, executed_at DESC);
CREATE INDEX IF NOT EXISTS idx_trades_maker_acc ON trades(maker_account_id);
CREATE INDEX IF NOT EXISTS idx_trades_taker_acc ON trades(taker_account_id);

CREATE TABLE IF NOT EXISTS ledger_transactions (
    tx_id VARCHAR(64) PRIMARY KEY,
    trade_id VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ledger_entries (
    entry_id VARCHAR(64) PRIMARY KEY,
    tx_id VARCHAR(64) NOT NULL REFERENCES ledger_transactions(tx_id) ON DELETE CASCADE,
    account_id VARCHAR(64) NOT NULL REFERENCES accounts(account_id),
    entry_type VARCHAR(16) NOT NULL CHECK (entry_type IN ('DEBIT', 'CREDIT')),
    asset_type VARCHAR(32) NOT NULL CHECK (asset_type IN ('CASH_VND', 'STOCK_SHARE')),
    symbol VARCHAR(16),
    amount BIGINT NOT NULL CHECK (amount > 0),
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_account ON ledger_entries(account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_tx ON ledger_entries(tx_id);

-- 2. Row Level Security (RLS) Configuration
ALTER TABLE accounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE account_holdings ENABLE ROW LEVEL SECURITY;
ALTER TABLE orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE trades ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_transactions ENABLE ROW LEVEL SECURITY;
ALTER TABLE ledger_entries ENABLE ROW LEVEL SECURITY;

-- Allow full access to service_role (backend services) and postgres superuser
DO $$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'service_role_all_accounts') THEN
        CREATE POLICY service_role_all_accounts ON accounts FOR ALL TO service_role USING (true) WITH CHECK (true);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'service_role_all_holdings') THEN
        CREATE POLICY service_role_all_holdings ON account_holdings FOR ALL TO service_role USING (true) WITH CHECK (true);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'service_role_all_orders') THEN
        CREATE POLICY service_role_all_orders ON orders FOR ALL TO service_role USING (true) WITH CHECK (true);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'service_role_all_trades') THEN
        CREATE POLICY service_role_all_trades ON trades FOR ALL TO service_role USING (true) WITH CHECK (true);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'service_role_all_tx') THEN
        CREATE POLICY service_role_all_tx ON ledger_transactions FOR ALL TO service_role USING (true) WITH CHECK (true);
    END IF;
    IF NOT EXISTS (SELECT 1 FROM pg_policies WHERE policyname = 'service_role_all_entries') THEN
        CREATE POLICY service_role_all_entries ON ledger_entries FOR ALL TO service_role USING (true) WITH CHECK (true);
    END IF;
END $$;
