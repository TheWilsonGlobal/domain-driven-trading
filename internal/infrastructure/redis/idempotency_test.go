package redis_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/redis"
)

var _ ports.IdempotencyStore = (*redis.RedisIdempotencyStore)(nil)

func newStore(t *testing.T) (*redis.RedisIdempotencyStore, *miniredis.Miniredis) {
	t.Helper()
	mr := miniredis.RunT(t)
	c := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { c.Close() })
	return redis.NewRedisIdempotencyStore(c), mr
}

func TestRedisStore_ClaimCompleteReplayRelease(t *testing.T) {
	ctx := context.Background()
	s, _ := newStore(t)

	ok, _, err := s.Claim(ctx, "idem:A:c1", "fp", time.Minute)
	if err != nil || !ok {
		t.Fatalf("claim = %v, %v", ok, err)
	}
	ok, rec, err := s.Claim(ctx, "idem:A:c1", "fp", time.Minute)
	if err != nil || ok || rec.Done {
		t.Fatalf("second claim = ok=%v rec=%+v err=%v (want in-flight)", ok, rec, err)
	}
	if err := s.Complete(ctx, "idem:A:c1", ports.IdempotencyRecord{Fingerprint: "fp", Status: 201, Body: []byte(`{"x":1}`)}, time.Hour); err != nil {
		t.Fatal(err)
	}
	ok, rec, err = s.Claim(ctx, "idem:A:c1", "fp", time.Minute)
	if err != nil || ok || !rec.Done || rec.Status != 201 || string(rec.Body) != `{"x":1}` || rec.Fingerprint != "fp" {
		t.Fatalf("replay = ok=%v rec=%+v err=%v", ok, rec, err)
	}

	if _, _, err := s.Claim(ctx, "idem:A:c2", "fp", time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := s.Release(ctx, "idem:A:c2"); err != nil {
		t.Fatal(err)
	}
	if ok, _, err := s.Claim(ctx, "idem:A:c2", "fp", time.Minute); err != nil || !ok {
		t.Fatalf("released key not claimable: %v %v", ok, err)
	}
}

func TestRedisStore_PendingClaimExpires(t *testing.T) {
	ctx := context.Background()
	s, mr := newStore(t)
	if ok, _, _ := s.Claim(ctx, "k", "fp", 30*time.Second); !ok {
		t.Fatal("claim failed")
	}
	mr.FastForward(31 * time.Second)
	if ok, _, _ := s.Claim(ctx, "k", "fp", 30*time.Second); !ok {
		t.Fatal("a crashed owner's claim must lapse")
	}
}

func TestRedisStore_ErrorsAreNotMaskedAndKeyIsNotClaimed(t *testing.T) {
	ctx := context.Background()
	s, mr := newStore(t)
	mr.Close() // Redis goes away

	ok, _, err := s.Claim(ctx, "k", "fp", time.Minute)
	if err == nil {
		t.Fatal("a Redis outage must surface as an error, not be masked by a local fallback")
	}
	if ok {
		t.Fatal("the key must not be claimed when Redis failed")
	}
	if err := s.Complete(ctx, "k", ports.IdempotencyRecord{Status: 201}, time.Minute); err == nil {
		t.Fatal("Complete must report the Redis error")
	}
	if err := s.Release(ctx, "k"); err == nil {
		t.Fatal("Release must report the Redis error")
	}
}

func TestConnect_WithPasswordAndUnreachable(t *testing.T) {
	mr := miniredis.RunT(t)
	mr.RequireAuth("s3cret-redis-pw")

	if _, err := redis.Connect(context.Background(), mr.Addr(), "bad-pw-xyz"); err == nil {
		t.Fatal("wrong password must fail the start")
	} else if strings.Contains(err.Error(), "bad-pw-xyz") || strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("password leaked: %v", err)
	}
	c, err := redis.Connect(context.Background(), mr.Addr(), "s3cret-redis-pw")
	if err != nil {
		t.Fatal(err)
	}
	c.Close()

	addr := mr.Addr()
	mr.Close()
	if _, err := redis.Connect(context.Background(), addr, "s3cret-redis-pw"); err == nil || !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("err = %v, want an 'unreachable' error", err)
	}
}
