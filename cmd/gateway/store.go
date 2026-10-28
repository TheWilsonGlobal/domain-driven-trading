package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type storeMode string

const (
	storePostgres storeMode = "postgres"
	storeMemory   storeMode = "memory"
)

// resolveStore decides where state lives, explicitly (decision D5):
//
//	STORE=memory   -> in-memory, only when asked for (the caller logs it)
//	STORE=postgres -> PostgreSQL; DATABASE_URL is required
//	STORE unset    -> postgres when DATABASE_URL is set, otherwise an error:
//	                  there is no silent fallback to memory.
func resolveStore(getenv func(string) string) (storeMode, error) {
	url := strings.TrimSpace(getenv("DATABASE_URL"))
	switch v := strings.ToLower(strings.TrimSpace(getenv("STORE"))); v {
	case "":
		if url == "" {
			return "", errors.New("no store configured: set DATABASE_URL (PostgreSQL) or STORE=memory explicitly")
		}
		return storePostgres, nil
	case string(storePostgres):
		if url == "" {
			return "", errors.New("STORE=postgres requires DATABASE_URL")
		}
		return storePostgres, nil
	case string(storeMemory):
		return storeMemory, nil
	default:
		return "", fmt.Errorf("unknown STORE %q (want postgres or memory)", v)
	}
}

// connectPool opens the pool and pings it: an unreachable database fails the
// start with a clear error instead of degrading to memory. The URL itself is
// never put in the error (it carries the password).
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
