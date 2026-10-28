// Package seed creates the starter accounts. Seeding is an explicit step run by
// the composition root; repositories never invent accounts on their own.
package seed

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

// Account describes one starter account.
type Account struct {
	ID       string
	Cash     int64
	Holdings map[order.Symbol]uint32
}

// Defaults is the local-development seed set used when SEED_ACCOUNTS is unset.
func Defaults() []Account {
	return []Account{
		{ID: "ACC_APX_001", Cash: 500_000_000, Holdings: map[order.Symbol]uint32{"APX": 10000, "HPG": 5000}},
		{ID: "ACC_APX_002", Cash: 300_000_000, Holdings: map[order.Symbol]uint32{"APX": 8000}},
		{ID: "ACC_INST_MM", Cash: 20_000_000_000, Holdings: map[order.Symbol]uint32{"APX": 100000, "HPG": 100000, "FPT": 50000}},
		{ID: account.FeeCollectorID, Cash: 0},
	}
}

// FromEnv returns the accounts in SEED_ACCOUNTS, or Defaults when it is unset.
//
// Format: ID:cash[:SYM=qty,SYM=qty][;ID:cash...]  (cash in VND).
func FromEnv(getenv func(string) string) ([]Account, error) {
	raw := strings.TrimSpace(getenv("SEED_ACCOUNTS"))
	if raw == "" {
		return Defaults(), nil
	}
	return Parse(raw)
}

// Parse parses the SEED_ACCOUNTS format.
func Parse(raw string) ([]Account, error) {
	var out []Account
	seen := map[string]bool{}
	for _, item := range strings.Split(raw, ";") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		parts := strings.Split(item, ":")
		if len(parts) < 2 || len(parts) > 3 {
			return nil, fmt.Errorf("seed %q: want ID:cash[:SYM=qty,...]", item)
		}
		id := parts[0]
		if !command.ValidID(id) {
			return nil, fmt.Errorf("seed %q: malformed account id", item)
		}
		if seen[id] {
			return nil, fmt.Errorf("seed: duplicate account %q", id)
		}
		seen[id] = true
		cash, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil || cash < 0 {
			return nil, fmt.Errorf("seed %q: cash must be a non-negative integer", id)
		}
		acc := Account{ID: id, Cash: cash}
		if len(parts) == 3 && parts[2] != "" {
			acc.Holdings = map[order.Symbol]uint32{}
			for _, h := range strings.Split(parts[2], ",") {
				sym, qtyStr, ok := strings.Cut(h, "=")
				qty, err := strconv.ParseUint(qtyStr, 10, 32)
				if !ok || sym == "" || err != nil {
					return nil, fmt.Errorf("seed %q: holding %q must be SYM=qty", id, h)
				}
				acc.Holdings[order.Symbol(sym)] = uint32(qty)
			}
		}
		out = append(out, acc)
	}
	if len(out) == 0 {
		return nil, errors.New("seed: SEED_ACCOUNTS has no accounts")
	}
	return out, nil
}

// Accounts creates each listed account that does not exist yet and returns how
// many it created. It is idempotent and never overwrites an existing account.
// FEE_COLLECTOR is always ensured (with zero cash) because settlement needs it.
// A repository error other than "not found" aborts the run.
func Accounts(ctx context.Context, repo ports.AccountRepository, list []Account, logger *slog.Logger) (int, error) {
	if logger == nil {
		logger = slog.Default()
	}
	hasCollector := false
	for _, a := range list {
		if a.ID == account.FeeCollectorID {
			hasCollector = true
		}
	}
	if !hasCollector {
		list = append(append([]Account(nil), list...), Account{ID: account.FeeCollectorID})
	}

	created := 0
	for _, a := range list {
		_, err := repo.GetByID(ctx, a.ID)
		if err == nil {
			logger.Info("seed: account exists, left unchanged", "account", a.ID)
			continue
		}
		if !errors.Is(err, common.ErrAccountNotFound) {
			return created, fmt.Errorf("seed %s: %w", a.ID, err)
		}
		acc := account.NewAccount(a.ID, a.Cash)
		for sym, qty := range a.Holdings {
			acc.CreditStock(sym, qty)
		}
		if err := repo.Save(ctx, acc); err != nil {
			return created, fmt.Errorf("seed %s: %w", a.ID, err)
		}
		created++
		logger.Info("seed: account created", "account", a.ID, "cash", a.Cash, "symbols", len(a.Holdings))
	}
	return created, nil
}
