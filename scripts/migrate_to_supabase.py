"""
migrate_to_supabase.py
Automated database migration and verification runner for Domain-Driven Trading Engine.
Connects to Supabase PostgreSQL, provisions schema, configures RLS, seeds accounts,
and verifies mathematical invariants.
"""

import os
import sys
import psycopg2
from psycopg2.extras import execute_values

DB_CONFIG = {
    'host': os.getenv('SUPABASE_DB_HOST', 'db.dxbbbhwqokvfsedoiths.supabase.co'),
    'port': int(os.getenv('SUPABASE_DB_PORT', '5432')),
    'user': os.getenv('SUPABASE_DB_USER', 'postgres'),
    'password': os.getenv('SUPABASE_DB_PASSWORD', 'P@ssw0rd123ABC#%^^$%#'),
    'dbname': os.getenv('SUPABASE_DB_NAME', 'postgres'),
    'sslmode': 'require',
}

# Fallback pooler config if direct host is unreachable
POOLER_CONFIG = {
    'host': 'aws-0-ap-northeast-1.pooler.supabase.com',
    'port': 5432,
    'user': 'postgres.dxbbbhwqokvfsedoiths',
    'password': 'P@ssw0rd123ABC#%^^$%#',
    'dbname': 'postgres',
    'sslmode': 'require',
}

def get_connection():
    try:
        print(f"Connecting to Supabase Direct: {DB_CONFIG['host']}:{DB_CONFIG['port']}...")
        conn = psycopg2.connect(**DB_CONFIG)
        print("  -> Direct connection succeeded!")
        return conn
    except Exception as e:
        print(f"  -> Direct connection failed ({e}). Falling back to Session Pooler...")
        conn = psycopg2.connect(**POOLER_CONFIG)
        print("  -> Session Pooler connection succeeded!")
        return conn

def run_migration():
    conn = get_connection()
    conn.autocommit = True
    cur = conn.cursor()

    # Step 1: Read and apply DDL
    ddl_path = os.path.join(
        os.path.dirname(__file__), '..', 'internal', 'infrastructure', 'persistence', 'migrations', '002_supabase_init.sql'
    )
    if not os.path.exists(ddl_path):
        ddl_path = os.path.join(
            os.path.dirname(__file__), '..', 'internal', 'infrastructure', 'persistence', 'migrations', '001_init.sql'
        )

    print(f"\n[1/4] Applying Schema DDL & RLS from {os.path.basename(ddl_path)}...")
    with open(ddl_path, 'r', encoding='utf-8') as f:
        ddl_sql = f.read()

    cur.execute(ddl_sql)
    print("  -> DDL & RLS policies successfully applied.")

    # Step 2: Seed Accounts
    print("\n[2/4] Ingesting Seed Accounts & Institutional Positions...")
    accounts_seed = [
        ('ACC_APX_001', 500000000, 0),     # 500M VND
        ('ACC_APX_002', 300000000, 0),     # 300M VND
        ('ACC_INST_MM', 20000000000, 0),   # 20B VND Market Maker
        ('FEE_COLLECTOR', 0, 0)
    ]
    cur.execute("""
        INSERT INTO accounts (account_id, cash_balance, locked_cash)
        VALUES (%s, %s, %s)
        ON CONFLICT (account_id) DO UPDATE 
        SET cash_balance = EXCLUDED.cash_balance, locked_cash = EXCLUDED.locked_cash;
    """, accounts_seed[0])
    for acc in accounts_seed[1:]:
        cur.execute("""
            INSERT INTO accounts (account_id, cash_balance, locked_cash)
            VALUES (%s, %s, %s)
            ON CONFLICT (account_id) DO UPDATE 
            SET cash_balance = EXCLUDED.cash_balance, locked_cash = EXCLUDED.locked_cash;
        """, acc)

    holdings_seed = [
        ('ACC_APX_001', 'APX', 10000, 0),
        ('ACC_APX_001', 'HPG', 5000, 0),
        ('ACC_APX_002', 'APX', 8000, 0),
        ('ACC_INST_MM', 'APX', 100000, 0),
        ('ACC_INST_MM', 'HPG', 100000, 0),
        ('ACC_INST_MM', 'FPT', 50000, 0),
    ]
    for h in holdings_seed:
        cur.execute("""
            INSERT INTO account_holdings (account_id, symbol, quantity, locked_quantity)
            VALUES (%s, %s, %s, %s)
            ON CONFLICT (account_id, symbol) DO UPDATE
            SET quantity = EXCLUDED.quantity, locked_quantity = EXCLUDED.locked_quantity;
        """, h)
    print("  -> Seed accounts & holdings populated.")

    # Step 3: Verification & Invariant Checks
    print("\n[3/4] Verifying Invariants and Schema in Supabase...")
    cur.execute("SELECT count(*) FROM accounts;")
    acc_count = cur.fetchone()[0]

    cur.execute("SELECT count(*) FROM account_holdings;")
    hld_count = cur.fetchone()[0]

    cur.execute("SELECT sum(cash_balance) FROM accounts;")
    total_cash = cur.fetchone()[0]

    print(f"  -> Total Accounts in Supabase: {acc_count}")
    print(f"  -> Total Holdings Records:     {hld_count}")
    print(f"  -> Total Seed Cash Pool:       {total_cash:,.0f} VND")

    # Double-Entry Balance Check
    cur.execute("""
        SELECT 
            asset_type,
            SUM(CASE WHEN entry_type = 'DEBIT' THEN amount ELSE -amount END) AS net_imbalance
        FROM ledger_entries
        GROUP BY asset_type;
    """)
    ledger_imbalances = cur.fetchall()
    if not ledger_imbalances:
        print("  -> Ledger entries empty (fresh baseline). Double-entry invariant is trivially satisfied.")
    else:
        for asset, imb in ledger_imbalances:
            print(f"  -> Ledger balance for {asset}: net imbalance = {imb}")
            assert imb == 0, f"Critical invariant failure: unbalanced books for {asset}!"

    # Step 4: Verify RLS is active
    print("\n[4/4] Verifying Row-Level Security (RLS) on all tables...")
    cur.execute("""
        SELECT tablename, rowsecurity 
        FROM pg_tables 
        WHERE schemaname = 'public' 
          AND tablename IN ('accounts', 'account_holdings', 'orders', 'trades', 'ledger_transactions', 'ledger_entries')
        ORDER BY tablename;
    """)
    rls_status = cur.fetchall()
    all_rls_enabled = True
    for table, enabled in rls_status:
        status_str = "ENABLED [OK]" if enabled else "DISABLED [FAIL]"
        print(f"  -> Table '{table}': RLS is {status_str}")
        if not enabled:
            all_rls_enabled = False

    conn.close()
    print("\n" + "="*70)
    if all_rls_enabled and acc_count >= 4:
        print("[SUCCESS] Supabase PostgreSQL migration & seeding completed flawlessly!")
    else:
        print("[WARNING] Migration completed with pending checks.")
    print("="*70 + "\n")

if __name__ == '__main__':
    run_migration()
