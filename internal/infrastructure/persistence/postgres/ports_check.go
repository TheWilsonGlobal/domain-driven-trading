package postgres

import "github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"

// Compile-time proof that the adapters satisfy the application ports.
var (
	_ ports.AccountRepository      = (*PostgresAccountRepository)(nil)
	_ ports.CashReserver           = (*PostgresAccountRepository)(nil)
	_ ports.OrderRepository        = (*PostgresOrderRepository)(nil)
	_ ports.TradeJournalRepository = (*PostgresTradeJournalRepository)(nil)
)
