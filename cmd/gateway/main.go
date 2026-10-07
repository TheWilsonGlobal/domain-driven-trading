package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
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
	httpTransport "github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/transport/http"
	wsTransport "github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/transport/websocket"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	symbol := order.Symbol("APX")
	orderBook := order.NewOrderBook(symbol)
	matcher := order.NewMatcher(orderBook)

	accountRepo := postgres.NewPostgresAccountRepository(nil)
	idempotency := redis.NewRedisIdempotencyStore(nil)
	eventPublisher, _ := kafka.NewInMemoryPublisher()

	placeOrderHandler := command.NewPlaceOrderHandler(matcher, accountRepo, eventPublisher, idempotency)
	cancelOrderHandler := command.NewCancelOrderHandler(matcher, accountRepo, eventPublisher)
	orderBookHandler := query.NewGetOrderBookHandler(matcher)
	balanceHandler := query.NewGetBalanceHandler(accountRepo)

	// Seed maker orders
	ctx := context.Background()
	_, _ = placeOrderHandler.Handle(ctx, command.PlaceOrderCommand{
		ClientOrderID: "SEED_BID_1",
		AccountID:     "ACC_INST_MM",
		Symbol:        symbol,
		Side:          order.SideBuy,
		Type:          order.TypeLimit,
		Price:         19450,
		Quantity:      5000,
	})
	_, _ = placeOrderHandler.Handle(ctx, command.PlaceOrderCommand{
		ClientOrderID: "SEED_ASK_1",
		AccountID:     "ACC_INST_MM",
		Symbol:        symbol,
		Side:          order.SideSell,
		Type:          order.TypeLimit,
		Price:         19500,
		Quantity:      5000,
	})

	wsHub := wsTransport.NewMarketHub(logger)
	go wsHub.Run()

	httpHandler := httpTransport.NewHTTPHandler(
		placeOrderHandler,
		cancelOrderHandler,
		orderBookHandler,
		balanceHandler,
	)

	mux := http.NewServeMux()
	mux.Handle("/", httpHandler.Router())
	mux.HandleFunc("/ws/market", wsHub.HandleWS)

	server := &http.Server{
		Addr:         fmt.Sprintf(":%s", port),
		Handler:      mux,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	go func() {
		logger.Info("domain-driven-trading gateway HTTP & WS server listening", "port", port)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server failed", "error", err)
		}
	}()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = server.Shutdown(shutdownCtx)
	logger.Info("gateway server shutdown cleanly")
}
