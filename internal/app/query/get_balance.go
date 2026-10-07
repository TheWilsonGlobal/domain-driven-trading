package query

import (
	"context"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
)

type GetBalanceQuery struct {
	AccountID string
}

type GetBalanceHandler struct {
	repo ports.AccountRepository
}

func NewGetBalanceHandler(repo ports.AccountRepository) *GetBalanceHandler {
	return &GetBalanceHandler{
		repo: repo,
	}
}

func (h *GetBalanceHandler) Handle(ctx context.Context, q GetBalanceQuery) (*account.Account, error) {
	return h.repo.GetByID(ctx, q.AccountID)
}
