package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/persistence/memory"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/redis"
)

type idemMode string

const (
	idemRedis  idemMode = "redis"
	idemMemory idemMode = "memory"
)

// resolveIdempotency decides where idempotency keys live, explicitly (D5):
//
//	IDEMPOTENCY=memory -> bounded in-memory store, only when asked for
//	IDEMPOTENCY=redis  -> Redis; REDIS_ADDR is required
//	IDEMPOTENCY unset  -> Redis when REDIS_ADDR is set, otherwise an error
func resolveIdempotency(getenv func(string) string) (idemMode, error) {
	addr := strings.TrimSpace(getenv("REDIS_ADDR"))
	switch v := strings.ToLower(strings.TrimSpace(getenv("IDEMPOTENCY"))); v {
	case "":
		if addr == "" {
			return "", errors.New("no idempotency store configured: set REDIS_ADDR (Redis) or IDEMPOTENCY=memory explicitly")
		}
		return idemRedis, nil
	case string(idemRedis):
		if addr == "" {
			return "", errors.New("IDEMPOTENCY=redis requires REDIS_ADDR")
		}
		return idemRedis, nil
	case string(idemMemory):
		return idemMemory, nil
	default:
		return "", fmt.Errorf("unknown IDEMPOTENCY %q (want redis or memory)", v)
	}
}

// openIdempotency builds the store for mode. Redis is pinged at start: an
// unreachable Redis stops the gateway. The returned func releases resources.
func openIdempotency(ctx context.Context, mode idemMode, getenv func(string) string, logger *slog.Logger) (ports.IdempotencyStore, func(), error) {
	switch mode {
	case idemRedis:
		client, err := redis.Connect(ctx, strings.TrimSpace(getenv("REDIS_ADDR")), getenv("REDIS_PASSWORD"))
		if err != nil {
			return nil, nil, err
		}
		return redis.NewRedisIdempotencyStore(client), func() { _ = client.Close() }, nil
	case idemMemory:
		logger.Warn("IDEMPOTENCY=memory: order de-duplication is process-local, bounded and lost on restart")
		store := memory.NewIdempotencyStore(memory.DefaultIdempotencyEntries)
		sweepCtx, stop := context.WithCancel(ctx)
		go func() {
			t := time.NewTicker(time.Minute)
			defer t.Stop()
			for {
				select {
				case <-sweepCtx.Done():
					return
				case <-t.C:
					store.Sweep()
				}
			}
		}()
		return store, stop, nil
	default:
		return nil, nil, fmt.Errorf("unknown idempotency mode %q", mode)
	}
}
