package redis

import (
	"context"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

type RedisIdempotencyStore struct {
	client *redis.Client
	// In-memory fallback if Redis is unavailable
	fallbackMu sync.Mutex
	fallbackMap map[string]time.Time
}

func NewRedisIdempotencyStore(client *redis.Client) *RedisIdempotencyStore {
	return &RedisIdempotencyStore{
		client:      client,
		fallbackMap: make(map[string]time.Time),
	}
}

func (s *RedisIdempotencyStore) CheckAndSet(ctx context.Context, key string, ttlSeconds int) (bool, error) {
	if s.client != nil {
		// SET key value NX EX ttl
		ok, err := s.client.SetNX(ctx, key, "1", time.Duration(ttlSeconds)*time.Second).Result()
		if err == nil {
			return ok, nil
		}
	}

	// In-memory fallback
	s.fallbackMu.Lock()
	defer s.fallbackMu.Unlock()

	now := time.Now()
	if exp, exists := s.fallbackMap[key]; exists && now.Before(exp) {
		return false, nil // Duplicate found!
	}

	s.fallbackMap[key] = now.Add(time.Duration(ttlSeconds) * time.Second)
	return true, nil
}
