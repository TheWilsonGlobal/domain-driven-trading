// Command engine is a single-process, in-memory matching core for local
// experiments and smoke runs. It is NOT the production path.
//
// Role: it composes the same core as cmd/gateway (internal/bootstrap) but with
// in-memory accounts, orders and event bus, and exposes no network API. Events
// never leave the process and all state is lost on exit. The durable, served
// system is cmd/gateway (HTTP + websocket) plus cmd/settlement.
//
// Deprecated: kept only so the existing compose service and Dockerfile target
// keep building; use cmd/gateway (STORE=memory BUS=memory for a no-dependency
// run). A follow-up may remove this command and its compose service.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/seed"
	"github.com/domain-driven-trading/domain-driven-trading/internal/bootstrap"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/kafka"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/persistence/memory"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/persistence/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	logger.Warn("cmd/engine is deprecated: in-memory state, in-memory event bus, no API; use cmd/gateway")
	logger.Info("starting domain-driven-trading matching core...", "version", "1.0.0-poc")

	ctx := context.Background()
	accountRepo := postgres.NewPostgresAccountRepository(nil) // nil pool = in-memory
	seedList, err := seed.FromEnv(os.Getenv)
	if err != nil {
		logger.Error("invalid SEED_ACCOUNTS", "error", err)
		os.Exit(1)
	}
	if _, err := seed.Accounts(ctx, accountRepo, seedList, logger); err != nil {
		logger.Error("account seeding failed", "error", err)
		os.Exit(1)
	}
	eventPublisher, _ := kafka.NewInMemoryPublisher()

	core := bootstrap.NewCore(bootstrap.NewMatcher(), bootstrap.Ports{
		Accounts:  accountRepo,
		Orders:    memory.NewOrderRepository(),
		Journal:   memory.NewTradeJournalRepository(),
		Publisher: eventPublisher,
	})

	placed, err := core.SeedMakers(ctx, nil, logger)
	if err != nil {
		logger.Error("maker seeding failed", "error", err)
		os.Exit(1)
	}
	logger.Info("matching core initialized with continuous price-time priority", "symbol", core.Symbol, "maker_orders", placed)

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("shutting down matching engine gracefully...")
	_ = eventPublisher.Close()
	time.Sleep(100 * time.Millisecond)
	logger.Info("engine shutdown complete")
}
