package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/seed"
	"github.com/domain-driven-trading/domain-driven-trading/internal/bootstrap"
	"github.com/domain-driven-trading/domain-driven-trading/internal/config"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/auth"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/kafka"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/persistence/memory"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/persistence/postgres"
	httpTransport "github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/transport/http"
	wsTransport "github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/transport/websocket"
)

// newHTTPServer builds the listener with timeouts that bound slow-header
// (slowloris), slow-body and idle connections, and a header size cap well
// below net/http's 1 MiB default. WriteTimeout does not apply to /ws/market
// once the connection is hijacked; the hub sets its own write deadlines.
// loopbackOnly (unauthenticated dev mode) binds 127.0.0.1 instead of every
// interface so the header-trusting mode is never reachable from the network.
func newHTTPServer(port string, handler http.Handler, loopbackOnly bool) *http.Server {
	addr := ":" + port
	if loopbackOnly {
		addr = "127.0.0.1:" + port
	}
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}

func envPositiveInt(getenv func(string) string, name string, def int) (int, error) {
	raw := getenv(name)
	if raw == "" {
		return def, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("%s %q must be a positive integer", name, raw)
	}
	return n, nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	port := os.Getenv("PORT")
	if port == "" {
		port = config.DefaultPort
	}

	// Fail fast on a bad auth configuration (unknown mode, jwt without
	// JWKS_URL / JWT_ISSUER / JWT_AUDIENCE) before anything is opened.
	authCfg, err := auth.LoadConfig(os.Getenv)
	if err != nil {
		logger.Error("invalid auth configuration", "error", err)
		os.Exit(1)
	}
	authMiddleware, err := auth.NewMiddleware(authCfg, logger)
	if err != nil {
		logger.Error("auth setup failed", "error", err)
		os.Exit(1)
	}
	seedList, err := seed.FromEnv(os.Getenv)
	if err != nil {
		logger.Error("invalid SEED_ACCOUNTS", "error", err)
		os.Exit(1)
	}

	matcher := bootstrap.NewMatcher()

	// Where state lives is an explicit decision (D5): PostgreSQL when
	// DATABASE_URL is set, in-memory only for STORE=memory. An unreachable
	// database stops the start; there is no silent in-memory fallback.
	mode, err := resolveStore(os.Getenv)
	if err != nil {
		logger.Error("invalid store configuration", "error", err)
		os.Exit(1)
	}
	ctx := context.Background()

	var (
		accountRepo ports.AccountRepository
		orderRepo   ports.OrderRepository
		journalRepo ports.TradeJournalRepository
	)
	switch mode {
	case storePostgres:
		pool, err := connectPool(ctx, os.Getenv("DATABASE_URL"))
		if err != nil {
			logger.Error("cannot start: PostgreSQL is configured but unusable", "error", err)
			os.Exit(1)
		}
		defer pool.Close()
		logger.Info("gateway connected to PostgreSQL", "store", string(mode))
		accountRepo = postgres.NewPostgresAccountRepository(pool)
		orderRepo = postgres.NewPostgresOrderRepository(pool)
		journalRepo = postgres.NewPostgresTradeJournalRepository(pool)
	case storeMemory:
		logger.Warn("STORE=memory: accounts, orders and trades live in memory only and are lost on restart")
		accountRepo = postgres.NewPostgresAccountRepository(nil)
		orderRepo = memory.NewOrderRepository()
		journalRepo = memory.NewTradeJournalRepository()
	}

	if n, err := seed.Accounts(ctx, accountRepo, seedList, logger); err != nil {
		logger.Error("account seeding failed", "error", err)
		os.Exit(1)
	} else {
		logger.Info("account seeding complete", "created", n, "configured", len(seedList))
	}

	// Recovery (plan 5.4): rebuild the book and the locks from the persisted
	// open orders after seeding and before serving.
	recovery, err := command.RecoverBook(ctx, matcher, orderRepo, accountRepo, logger)
	if err != nil {
		logger.Error("recovery failed", "error", err)
		os.Exit(1)
	}

	// Idempotency store and event bus are explicit decisions too (D5): Redis
	// from REDIS_ADDR / Kafka from KAFKA_BROKERS, in-memory only for
	// IDEMPOTENCY=memory / BUS=memory. Unreachable ones stop the start.
	idemMode, err := resolveIdempotency(os.Getenv)
	if err != nil {
		logger.Error("invalid idempotency configuration", "error", err)
		os.Exit(1)
	}
	idempotency, closeIdem, err := openIdempotency(ctx, idemMode, os.Getenv, logger)
	if err != nil {
		logger.Error("cannot start: idempotency store is configured but unusable", "error", err)
		os.Exit(1)
	}
	defer closeIdem()

	busCfg, err := kafka.ResolveBus(os.Getenv)
	if err != nil {
		logger.Error("invalid event bus configuration", "error", err)
		os.Exit(1)
	}
	bus, err := kafka.OpenBus(busCfg, logger)
	if err != nil {
		logger.Error("cannot start: event bus is configured but unusable", "error", err)
		os.Exit(1)
	}
	logger.Info("event bus ready", "bus", string(bus.Mode()))
	eventPublisher := kafka.NewWatermillPublisher(bus.Publisher(), logger)

	// Websocket feed (plan 6.4): the hub is fed from the bus, not from the
	// request path. The feed uses a group-less subscriber so every gateway
	// instance sees every event.
	webCfg, err := httpTransport.ConfigFromEnv(os.Getenv)
	if err != nil {
		logger.Error("invalid HTTP configuration", "error", err)
		os.Exit(1)
	}
	wsMax, err := envPositiveInt(os.Getenv, "WS_MAX_CLIENTS", config.DefaultWSMaxClients)
	if err != nil {
		logger.Error("invalid WS_MAX_CLIENTS", "error", err)
		os.Exit(1)
	}
	wsHub := wsTransport.NewMarketHub(logger).Configure(wsTransport.Options{
		AllowedOrigins: webCfg.AllowedOrigins,
		MaxClients:     wsMax,
	})
	go wsHub.Run()
	feedSub, err := bus.Subscriber("")
	if err != nil {
		logger.Error("cannot start: websocket feed subscriber failed", "error", err)
		os.Exit(1)
	}
	feedCtx, stopFeed := context.WithCancel(ctx)
	defer stopFeed()
	feedReady := make(chan struct{})
	go func() {
		if err := wsTransport.NewFeed(feedSub, wsHub, logger).Run(feedCtx, feedReady); err != nil && err != context.Canceled {
			logger.Error("websocket feed stopped", "error", err)
		}
	}()
	select {
	case <-feedReady:
	case <-time.After(10 * time.Second):
		logger.Error("cannot start: websocket feed did not subscribe within 10s")
		os.Exit(1)
	}

	core := bootstrap.NewCore(matcher, bootstrap.Ports{
		Accounts:  accountRepo,
		Orders:    orderRepo,
		Journal:   journalRepo,
		Publisher: eventPublisher,
	})

	// Seed maker orders once: a maker that already has open orders (restored
	// above) is skipped, so a restart does not duplicate the quotes.
	placed, err := core.SeedMakers(ctx, recovery.OpenByAccount, logger)
	if err != nil {
		logger.Error("maker seeding failed", "error", err)
		os.Exit(1)
	}
	logger.Info("maker seeding complete", "placed", placed, "restored_orders", recovery.Restored)

	httpHandler := httpTransport.NewHTTPHandler(
		core.Place,
		core.Cancel,
		core.OrderBook,
		core.Balance,
		authMiddleware,
	).WithIdempotency(idempotency).WithConfig(webCfg)

	mux := http.NewServeMux()
	mux.Handle("/", httpHandler.Router())
	mux.HandleFunc("/ws/market", wsHub.HandleWS)

	server := newHTTPServer(port, httpTransport.SecurityHeaders(mux), authCfg.Mode == auth.ModeDisabled)

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
	// Drain the publish queue (bounded wait), then close the bus.
	if err := eventPublisher.Close(); err != nil {
		logger.Warn("event publisher close", "error", err)
	}
	if n := eventPublisher.DeadLetterTotal(); n > 0 {
		logger.Error("events were dead-lettered during this run and are lost", "count", n)
	}
	_ = bus.Close()
	logger.Info("gateway server shutdown cleanly")
}
