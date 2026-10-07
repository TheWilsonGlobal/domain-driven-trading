-- 001_init.sql: PostgreSQL schema for Domain-Driven Trading Engine
-- Designed for high-frequency trading auditability, partition-ready ledgers, and trade persistence.

CREATE TABLE IF NOT EXISTS accounts (
    account_id VARCHAR(64) PRIMARY KEY,
    cash_balance BIGINT NOT NULL DEFAULT 0,
    locked_cash BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS account_holdings (
    account_id VARCHAR(64) NOT NULL REFERENCES accounts(account_id),
    symbol VARCHAR(16) NOT NULL,
    quantity BIGINT NOT NULL DEFAULT 0,
    locked_quantity BIGINT NOT NULL DEFAULT 0,
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
    price BIGINT NOT NULL,
    quantity INTEGER NOT NULL,
    filled_quantity INTEGER NOT NULL DEFAULT 0,
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
    maker_account_id VARCHAR(64) NOT NULL,
    taker_account_id VARCHAR(64) NOT NULL,
    taker_side VARCHAR(8) NOT NULL,
    price BIGINT NOT NULL,
    quantity INTEGER NOT NULL,
    executed_at TIMESTAMPTZ NOT NULL
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
    tx_id VARCHAR(64) NOT NULL REFERENCES ledger_transactions(tx_id),
    account_id VARCHAR(64) NOT NULL REFERENCES accounts(account_id),
    entry_type VARCHAR(16) NOT NULL, -- DEBIT, CREDIT
    asset_type VARCHAR(32) NOT NULL, -- CASH_VND, STOCK_SHARE
    symbol VARCHAR(16),
    amount BIGINT NOT NULL,
    description TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_ledger_entries_account ON ledger_entries(account_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_ledger_entries_tx ON ledger_entries(tx_id);

-- Seed initial test accounts with Apex Securities & institutional balance
INSERT INTO accounts (account_id, cash_balance, locked_cash)
VALUES 
    ('ACC_APX_001', 500000000, 0),    -- 500M VND
    ('ACC_APX_002', 300000000, 0),    -- 300M VND
    ('ACC_INST_MM', 20000000000, 0),  -- 20B VND Market Maker
    ('FEE_COLLECTOR', 0, 0)
ON CONFLICT (account_id) DO NOTHING;

INSERT INTO account_holdings (account_id, symbol, quantity, locked_quantity)
VALUES 
    ('ACC_APX_001', 'APX', 10000, 0),
    ('ACC_APX_001', 'HPG', 5000, 0),
    ('ACC_APX_002', 'APX', 8000, 0),
    ('ACC_INST_MM', 'APX', 100000, 0),
    ('ACC_INST_MM', 'HPG', 100000, 0),
    ('ACC_INST_MM', 'FPT', 50000, 0)
ON CONFLICT (account_id, symbol) DO NOTHING;
