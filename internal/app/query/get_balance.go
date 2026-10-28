package query

import (
	"context"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/common"
)

type GetBalanceQuery struct {
	AccountID string
}

// BalanceView is the read model returned by GetBalanceHandler. It is a plain
// value detached from the domain aggregate: no lock, no behaviour, and its maps
// are copies. The JSON shape is the public /v1/balance contract.
type BalanceView struct {
	AccountID    string                   `json:"account_id"`
	CashBalance  int64                    `json:"cash_balance"`
	LockedCash   int64                    `json:"locked_cash"`
	Holdings     map[common.Symbol]uint32 `json:"holdings"`
	LockedStocks map[common.Symbol]uint32 `json:"locked_stocks"`
	UpdatedAt    time.Time                `json:"updated_at"`
}

type GetBalanceHandler struct {
	repo ports.AccountRepository
}

func NewGetBalanceHandler(repo ports.AccountRepository) *GetBalanceHandler {
	return &GetBalanceHandler{
		repo: repo,
	}
}

func (h *GetBalanceHandler) Handle(ctx context.Context, q GetBalanceQuery) (*BalanceView, error) {
	acc, err := h.repo.GetByID(ctx, q.AccountID)
	if err != nil {
		return nil, err
	}
	s := acc.Snapshot() // copy taken under the account lock
	return &BalanceView{
		AccountID:    s.ID,
		CashBalance:  s.CashBalance,
		LockedCash:   s.LockedCash,
		Holdings:     s.Holdings,
		LockedStocks: s.LockedStocks,
		UpdatedAt:    s.UpdatedAt,
	}, nil
}
