package query

import (
	"context"

	"github.com/vortex-trading/vortex-trading-engine/internal/app/ports"
	"github.com/vortex-trading/vortex-trading-engine/internal/domain/account"
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
