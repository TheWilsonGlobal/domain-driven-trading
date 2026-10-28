package memory

import (
	"container/list"
	"context"
	"sync"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
)

// DefaultIdempotencyEntries bounds the in-memory idempotency store.
const DefaultIdempotencyEntries = 100_000

type idemEntry struct {
	key     string
	rec     ports.IdempotencyRecord
	expires time.Time
}

// IdempotencyStore is the explicit IDEMPOTENCY=memory implementation of
// ports.IdempotencyStore. It is bounded in two ways (plan L4 / 8.5): entries
// expire after their TTL, and when maxEntries is reached the oldest entry is
// evicted. It is process-local: it does not deduplicate across instances and
// is lost on restart, which is why the gateway only uses it when asked to.
type IdempotencyStore struct {
	mu         sync.Mutex
	maxEntries int
	now        func() time.Time
	items      map[string]*list.Element // value: *idemEntry
	order      *list.List               // front = oldest
}

func NewIdempotencyStore(maxEntries int) *IdempotencyStore {
	return NewIdempotencyStoreWithClock(maxEntries, time.Now)
}

func NewIdempotencyStoreWithClock(maxEntries int, now func() time.Time) *IdempotencyStore {
	if maxEntries <= 0 {
		maxEntries = DefaultIdempotencyEntries
	}
	return &IdempotencyStore{
		maxEntries: maxEntries,
		now:        now,
		items:      make(map[string]*list.Element),
		order:      list.New(),
	}
}

// live returns the unexpired entry for key, removing an expired one.
func (s *IdempotencyStore) live(key string) *idemEntry {
	el, ok := s.items[key]
	if !ok {
		return nil
	}
	e := el.Value.(*idemEntry)
	if !s.now().Before(e.expires) {
		s.order.Remove(el)
		delete(s.items, key)
		return nil
	}
	return e
}

func (s *IdempotencyStore) put(key string, rec ports.IdempotencyRecord, ttl time.Duration) {
	if el, ok := s.items[key]; ok {
		s.order.Remove(el)
		delete(s.items, key)
	}
	for len(s.items) >= s.maxEntries {
		oldest := s.order.Front()
		if oldest == nil {
			break
		}
		s.order.Remove(oldest)
		delete(s.items, oldest.Value.(*idemEntry).key)
	}
	s.items[key] = s.order.PushBack(&idemEntry{key: key, rec: rec, expires: s.now().Add(ttl)})
}

func (s *IdempotencyStore) Claim(_ context.Context, key, fingerprint string, pendingTTL time.Duration) (bool, ports.IdempotencyRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.live(key); e != nil {
		return false, e.rec, nil
	}
	s.put(key, ports.IdempotencyRecord{Fingerprint: fingerprint}, pendingTTL)
	return true, ports.IdempotencyRecord{}, nil
}

func (s *IdempotencyStore) Complete(_ context.Context, key string, rec ports.IdempotencyRecord, ttl time.Duration) error {
	rec.Done = true
	s.mu.Lock()
	defer s.mu.Unlock()
	s.put(key, rec, ttl)
	return nil
}

func (s *IdempotencyStore) Release(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if el, ok := s.items[key]; ok {
		s.order.Remove(el)
		delete(s.items, key)
	}
	return nil
}

// Len reports the number of stored entries (expired ones included until swept).
func (s *IdempotencyStore) Len() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.items)
}

// Sweep removes every expired entry. The gateway calls it on a timer.
func (s *IdempotencyStore) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for key, el := range s.items {
		if !now.Before(el.Value.(*idemEntry).expires) {
			s.order.Remove(el)
			delete(s.items, key)
		}
	}
}
