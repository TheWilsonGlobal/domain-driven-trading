package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/kafka"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/persistence/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// consumerGroup is shared by every settlement worker instance, so they split
// the partitions between them.
const consumerGroup = "settlement"

// Role: durable consumer of orders.events. The gateway already writes trades
// and the ledger journal in one transaction; this worker idempotently inserts
// the same trades (ON CONFLICT DO NOTHING) from the bus, so it tolerates the
// gateway having written them and recovers any row the gateway lost. See
// kafka.SettlementConsumer for the delivery contract.
func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	logger.Info("starting domain-driven-trading settlement worker...")

	url := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if url == "" {
		logger.Error("DATABASE_URL is required: the settlement worker has nowhere to persist trades without it")
		os.Exit(1)
	}
	busCfg, err := kafka.ResolveBus(os.Getenv)
	if err != nil {
		logger.Error("invalid event bus configuration", "error", err)
		os.Exit(1)
	}

	rootCtx := context.Background()
	pool, err := connectPool(rootCtx, url)
	if err != nil {
		logger.Error("cannot start: PostgreSQL is configured but unusable", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	logger.Info("settlement worker connected to PostgreSQL")

	bus, err := kafka.OpenBus(busCfg, logger)
	if err != nil {
		logger.Error("cannot start: event bus is configured but unusable", "error", err)
		os.Exit(1)
	}
	defer bus.Close()
	sub, err := bus.Subscriber(consumerGroup)
	if err != nil {
		logger.Error("cannot start: subscriber failed", "error", err)
		os.Exit(1)
	}

	consumer := kafka.NewSettlementConsumer(sub, postgres.NewPostgresTradeRepository(pool), logger).
		WithDeadLetter(bus.Publisher())

	ctx, cancel := context.WithCancel(rootCtx)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- consumer.Start(ctx) }()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sigChan:
		logger.Info("shutting down settlement worker...")
		cancel()
		// Start flushes the remaining batch with a fresh context before it returns.
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			logger.Error("consumer did not stop within 10s")
		}
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Error("consumer stopped with error", "error", err)
			os.Exit(1)
		}
	}
	logger.Info("settlement worker stopped")
}

// connectPool opens the pool and pings it; the URL (which carries the
// password) never appears in an error.
func connectPool(ctx context.Context, url string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, errors.New("DATABASE_URL is not a valid connection string")
	}
	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeSimpleProtocol
	cfg.MaxConns = 10
	cfg.MinConns = 2
	cfg.MaxConnLifetime = 30 * time.Minute
	cfg.MaxConnIdleTime = 5 * time.Minute

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("PostgreSQL at %s:%d is unreachable: %w", cfg.ConnConfig.Host, cfg.ConnConfig.Port, err)
	}
	return pool, nil
}
