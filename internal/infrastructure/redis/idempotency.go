package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
)

var errNoClient = errors.New("redis idempotency store has no client")

// Connect opens a Redis client and pings it, so an unreachable or
// misconfigured Redis stops the start instead of degrading silently. Neither
// the password nor the server's reply is placed in the returned error.
//
// addr is host:port, or a redis:// / rediss:// URL (the form Render Key Value
// hands out). A password in the URL wins; otherwise password is used.
func Connect(ctx context.Context, addr, password string) (*redis.Client, error) {
	opts, err := options(addr, password)
	if err != nil {
		return nil, err
	}
	opts.DialTimeout = 3 * time.Second
	opts.ReadTimeout = 2 * time.Second
	opts.WriteTimeout = 2 * time.Second
	opts.MaxRetries = 1
	c := redis.NewClient(opts)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := c.Ping(pingCtx).Err(); err != nil {
		_ = c.Close()
		var rerr redis.Error
		if errors.As(err, &rerr) {
			return nil, fmt.Errorf("Redis at %s rejected the connection (check REDIS_PASSWORD)", opts.Addr)
		}
		return nil, fmt.Errorf("Redis at %s is unreachable", opts.Addr)
	}
	return c, nil
}

// options turns REDIS_ADDR into client options. The URL form carries the
// password, so the error for a bad URL never repeats it.
func options(addr, password string) (*redis.Options, error) {
	if !strings.Contains(addr, "://") {
		return &redis.Options{Addr: addr, Password: password}, nil
	}
	opts, err := redis.ParseURL(addr)
	if err != nil {
		return nil, errors.New("REDIS_ADDR is not a valid redis:// or rediss:// URL")
	}
	if opts.Password == "" {
		opts.Password = password
	}
	return opts, nil
}

// RedisIdempotencyStore implements ports.IdempotencyStore on Redis. There is
// no local fallback: a Redis error is returned to the caller, which fails the
// request, and a key is never claimed on error.
type RedisIdempotencyStore struct {
	client *redis.Client
}

func NewRedisIdempotencyStore(client *redis.Client) *RedisIdempotencyStore {
	return &RedisIdempotencyStore{client: client}
}

type storedRecord struct {
	Fingerprint string `json:"fp"`
	Done        bool   `json:"done"`
	Status      int    `json:"status,omitempty"`
	Body        []byte `json:"body,omitempty"`
}

func encode(rec ports.IdempotencyRecord) ([]byte, error) {
	return json.Marshal(storedRecord(rec))
}

func (s *RedisIdempotencyStore) Claim(ctx context.Context, key, fingerprint string, pendingTTL time.Duration) (bool, ports.IdempotencyRecord, error) {
	if s.client == nil {
		return false, ports.IdempotencyRecord{}, errNoClient
	}
	val, err := encode(ports.IdempotencyRecord{Fingerprint: fingerprint})
	if err != nil {
		return false, ports.IdempotencyRecord{}, err
	}
	// The key can lapse between SETNX and GET; one more round settles it.
	for attempt := 0; attempt < 3; attempt++ {
		ok, err := s.client.SetNX(ctx, key, val, pendingTTL).Result()
		if err != nil {
			return false, ports.IdempotencyRecord{}, fmt.Errorf("idempotency claim: %w", err)
		}
		if ok {
			return true, ports.IdempotencyRecord{}, nil
		}
		raw, err := s.client.Get(ctx, key).Bytes()
		if errors.Is(err, redis.Nil) {
			continue
		}
		if err != nil {
			return false, ports.IdempotencyRecord{}, fmt.Errorf("idempotency read: %w", err)
		}
		var rec storedRecord
		if err := json.Unmarshal(raw, &rec); err != nil {
			return false, ports.IdempotencyRecord{}, fmt.Errorf("idempotency record is corrupt: %w", err)
		}
		return false, ports.IdempotencyRecord(rec), nil
	}
	return false, ports.IdempotencyRecord{}, errors.New("idempotency key kept changing during claim")
}

func (s *RedisIdempotencyStore) Complete(ctx context.Context, key string, rec ports.IdempotencyRecord, ttl time.Duration) error {
	if s.client == nil {
		return errNoClient
	}
	rec.Done = true
	val, err := encode(rec)
	if err != nil {
		return err
	}
	if err := s.client.Set(ctx, key, val, ttl).Err(); err != nil {
		return fmt.Errorf("idempotency complete: %w", err)
	}
	return nil
}

func (s *RedisIdempotencyStore) Release(ctx context.Context, key string) error {
	if s.client == nil {
		return errNoClient
	}
	if err := s.client.Del(ctx, key).Err(); err != nil {
		return fmt.Errorf("idempotency release: %w", err)
	}
	return nil
}
