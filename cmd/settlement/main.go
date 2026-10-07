package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/kafka"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/persistence/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	logger.Info("starting domain-driven-trading settlement worker...")

	dbURL := os.Getenv("DATABASE_URL")
	var pool *pgxpool.Pool
	var err error
	if dbURL != "" {
		pool, err = pgxpool.New(context.Background(), dbURL)
		if err != nil {
			logger.Warn("could not connect to PostgreSQL, falling back to mock batch logger", "err", err)
		}
	}

	tradeRepo := postgres.NewPostgresTradeRepository(pool)
	_, pubSub := kafka.NewInMemoryPublisher()

	consumer := kafka.NewSettlementConsumer(pubSub, tradeRepo, logger)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		if err := consumer.Start(ctx); err != nil && err != context.Canceled {
			logger.Error("consumer stopped with error", "err", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("shutting down settlement worker...")
	cancel()
	time.Sleep(200 * time.Millisecond)
	if pool != nil {
		pool.Close()
	}
	logger.Info("settlement worker stopped")
}
