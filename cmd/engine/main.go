package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/query"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/kafka"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/persistence/postgres"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/redis"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	logger.Info("starting domain-driven-trading matching core...", "version", "1.0.0-poc")

	symbol := order.Symbol("APX")
	orderBook := order.NewOrderBook(symbol)
	matcher := order.NewMatcher(orderBook)

	// In-memory repositories & event bus for resilient PoC startup
	accountRepo := postgres.NewPostgresAccountRepository(nil)
	idempotency := redis.NewRedisIdempotencyStore(nil)
	eventPublisher, _ := kafka.NewInMemoryPublisher()

	placeOrderHandler := command.NewPlaceOrderHandler(matcher, accountRepo, eventPublisher, idempotency)
	_ = command.NewCancelOrderHandler(matcher, accountRepo, eventPublisher)
	_ = query.NewGetOrderBookHandler(matcher)

	// Seed some initial resting maker liquidity
	ctx := context.Background()
	_, _ = placeOrderHandler.Handle(ctx, command.PlaceOrderCommand{
		ClientOrderID: "INIT_BID_1",
		AccountID:     "ACC_INST_MM",
		Symbol:        symbol,
		Side:          order.SideBuy,
		Type:          order.TypeLimit,
		Price:         19450,
		Quantity:      5000,
	})
	_, _ = placeOrderHandler.Handle(ctx, command.PlaceOrderCommand{
		ClientOrderID: "INIT_BID_2",
		AccountID:     "ACC_INST_MM",
		Symbol:        symbol,
		Side:          order.SideBuy,
		Type:          order.TypeLimit,
		Price:         19400,
		Quantity:      10000,
	})
	_, _ = placeOrderHandler.Handle(ctx, command.PlaceOrderCommand{
		ClientOrderID: "INIT_ASK_1",
		AccountID:     "ACC_INST_MM",
		Symbol:        symbol,
		Side:          order.SideSell,
		Type:          order.TypeLimit,
		Price:         19500,
		Quantity:      6000,
	})
	_, _ = placeOrderHandler.Handle(ctx, command.PlaceOrderCommand{
		ClientOrderID: "INIT_ASK_2",
		AccountID:     "ACC_INST_MM",
		Symbol:        symbol,
		Side:          order.SideSell,
		Type:          order.TypeLimit,
		Price:         19550,
		Quantity:      12000,
	})

	logger.Info("matching core initialized with continuous price-time priority", "symbol", symbol)

	// Graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("shutting down matching engine gracefully...")
	_ = eventPublisher.Close()
	time.Sleep(100 * time.Millisecond)
	logger.Info("engine shutdown complete")
}
