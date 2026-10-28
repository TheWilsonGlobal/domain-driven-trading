package http_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/query"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/account"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/auth"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/persistence/memory"
	httpt "github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/transport/http"
)

// spyStore wraps a real store and counts claims; errOn makes Claim fail.
type spyStore struct {
	inner  ports.IdempotencyStore
	claims int32
	errOn  error
}

func (s *spyStore) Claim(ctx context.Context, key, fp string, ttl time.Duration) (bool, ports.IdempotencyRecord, error) {
	atomic.AddInt32(&s.claims, 1)
	if s.errOn != nil {
		return false, ports.IdempotencyRecord{}, s.errOn
	}
	return s.inner.Claim(ctx, key, fp, ttl)
}
func (s *spyStore) Complete(ctx context.Context, key string, rec ports.IdempotencyRecord, ttl time.Duration) error {
	return s.inner.Complete(ctx, key, rec, ttl)
}
func (s *spyStore) Release(ctx context.Context, key string) error { return s.inner.Release(ctx, key) }

func newIdemEnv(t *testing.T, store ports.IdempotencyStore) *env {
	t.Helper()
	m := order.NewMatcher(order.NewOrderBook("APX"))
	acc := account.NewAccount("TRADER", 1_000_000_000)
	acc.CreditStock("APX", 100)
	rp := &repo{accs: map[string]*account.Account{
		"TRADER": acc, "OTHER": account.NewAccount("OTHER", 1_000_000), "FEE_COLLECTOR": account.NewAccount("FEE_COLLECTOR", 0),
	}}
	mw, err := auth.NewMiddleware(auth.Config{Mode: auth.ModeDisabled}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h := httpt.NewHTTPHandler(
		command.NewPlaceOrderHandler(m, rp, nil),
		command.NewCancelOrderHandler(m, rp, nil),
		query.NewGetOrderBookHandler(m),
		query.NewGetBalanceHandler(rp),
		mw,
	).WithIdempotency(store)
	srv := httptest.NewServer(h.Router())
	t.Cleanup(srv.Close)
	return &env{srv: srv, m: m, acc: acc, rp: rp}
}

func bidQty(e *env) uint32 {
	d := e.m.Depth(10)
	var n uint32
	for _, l := range d.Bids {
		n += l.TotalQuantity.Uint32()
	}
	return n
}

func postWith(t *testing.T, e *env, acct, header, body string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, e.srv.URL+"/v1/orders", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(auth.DevAccountHeader, acct)
	if header != "" {
		req.Header.Set("X-Idempotency-Key", header)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, string(b)
}

const buy10 = `{"symbol":"APX","side":"BUY","order_type":"LIMIT","price":19500,"quantity":10`

func TestIdempotency_KeyIsRequired(t *testing.T) {
	e := newIdemEnv(t, memory.NewIdempotencyStore(100))
	resp, body := postWith(t, e, "TRADER", "", buy10+`}`)
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d (%s), want 400 when neither header nor client_order_id is given", resp.StatusCode, body)
	}
	if bidQty(e) != 0 {
		t.Fatal("an order was placed without an idempotency key")
	}
}

func TestIdempotency_HeaderIsHonouredAndMustAgreeWithBody(t *testing.T) {
	e := newIdemEnv(t, memory.NewIdempotencyStore(100))
	resp, body := postWith(t, e, "TRADER", "hdr-1", buy10+`}`)
	if resp.StatusCode != 201 || !strings.Contains(body, `"client_order_id":"hdr-1"`) {
		t.Fatalf("header key: %d %s", resp.StatusCode, body)
	}
	resp, _ = postWith(t, e, "TRADER", "hdr-2", buy10+`,"client_order_id":"other"}`)
	if resp.StatusCode != 400 {
		t.Fatalf("header/body mismatch status = %d, want 400", resp.StatusCode)
	}
	resp, _ = postWith(t, e, "TRADER", "same", buy10+`,"client_order_id":"same"}`)
	if resp.StatusCode != 201 {
		t.Fatalf("matching header and body id = %d, want 201", resp.StatusCode)
	}
}

func TestIdempotency_ValidationHappensBeforeClaim(t *testing.T) {
	spy := &spyStore{inner: memory.NewIdempotencyStore(100)}
	e := newIdemEnv(t, spy)
	resp, _ := postWith(t, e, "TRADER", "", `{"client_order_id":"c1","symbol":"APX","side":"HOLD","order_type":"LIMIT","price":19500,"quantity":10}`)
	if resp.StatusCode != 400 {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if n := atomic.LoadInt32(&spy.claims); n != 0 {
		t.Fatalf("store consulted %d times for an invalid request", n)
	}
	// The key is still free: a corrected request with the same id succeeds.
	resp, _ = postWith(t, e, "TRADER", "", `{"client_order_id":"c1",`+buy10[1:]+`}`)
	if resp.StatusCode != 201 {
		t.Fatalf("retry after a rejected request = %d, want 201", resp.StatusCode)
	}
}

func TestIdempotency_ReplayReturnsTheOriginalResult(t *testing.T) {
	e := newIdemEnv(t, memory.NewIdempotencyStore(100))
	req := `{"client_order_id":"c1",` + buy10[1:] + `}`
	r1, b1 := postWith(t, e, "TRADER", "", req)
	r2, b2 := postWith(t, e, "TRADER", "", req)
	if r1.StatusCode != 201 || r2.StatusCode != 201 {
		t.Fatalf("statuses %d/%d, want 201/201 (original result, not 400/409)", r1.StatusCode, r2.StatusCode)
	}
	if b1 != b2 {
		t.Fatalf("replay body differs:\n%s\n%s", b1, b2)
	}
	if r2.Header.Get("Idempotent-Replay") != "true" {
		t.Fatal("replay is not flagged")
	}
	if got := bidQty(e); got != 10 {
		t.Fatalf("resting quantity = %d, want 10 (executed once)", got)
	}
}

func TestIdempotency_SameKeyDifferentBodyIsRejected(t *testing.T) {
	e := newIdemEnv(t, memory.NewIdempotencyStore(100))
	postWith(t, e, "TRADER", "", `{"client_order_id":"c1",`+buy10[1:]+`}`)
	resp, _ := postWith(t, e, "TRADER", "", `{"client_order_id":"c1","symbol":"APX","side":"BUY","order_type":"LIMIT","price":19500,"quantity":99}`)
	if resp.StatusCode != 422 {
		t.Fatalf("status = %d, want 422 for a reused key with a different request", resp.StatusCode)
	}
	if bidQty(e) != 10 {
		t.Fatal("the second, different order must not execute")
	}
}

func TestIdempotency_FailureReleasesTheClaim(t *testing.T) {
	e := newIdemEnv(t, memory.NewIdempotencyStore(100))
	// OTHER holds 1,000,000 VND: 1000 lots at 19,500 cannot be reserved.
	resp, _ := postWith(t, e, "OTHER", "", `{"client_order_id":"c9","symbol":"APX","side":"BUY","order_type":"LIMIT","price":19500,"quantity":1000}`)
	if resp.StatusCode != 422 {
		t.Fatalf("status = %d, want 422 insufficient balance", resp.StatusCode)
	}
	resp, _ = postWith(t, e, "OTHER", "", `{"client_order_id":"c9","symbol":"APX","side":"BUY","order_type":"LIMIT","price":19500,"quantity":5}`)
	if resp.StatusCode != 201 {
		t.Fatalf("retry with the same key after a failure = %d, want 201 (claim released)", resp.StatusCode)
	}
}

func TestIdempotency_KeyIsScopedToTheAccount(t *testing.T) {
	e := newIdemEnv(t, memory.NewIdempotencyStore(100))
	r1, _ := postWith(t, e, "TRADER", "", `{"client_order_id":"shared",`+buy10[1:]+`}`)
	r2, _ := postWith(t, e, "OTHER", "", `{"client_order_id":"shared","symbol":"APX","side":"BUY","order_type":"LIMIT","price":19500,"quantity":5}`)
	if r1.StatusCode != 201 || r2.StatusCode != 201 {
		t.Fatalf("statuses %d/%d, want 201/201 (key is idem:{account}:{id})", r1.StatusCode, r2.StatusCode)
	}
}

func TestIdempotency_StoreErrorFailsTheRequestAndPlacesNothing(t *testing.T) {
	spy := &spyStore{inner: memory.NewIdempotencyStore(100), errOn: errors.New("redis: connection refused")}
	e := newIdemEnv(t, spy)
	resp, body := postWith(t, e, "TRADER", "", `{"client_order_id":"c1",`+buy10[1:]+`}`)
	if resp.StatusCode != 503 {
		t.Fatalf("status = %d (%s), want 503", resp.StatusCode, body)
	}
	if strings.Contains(body, "redis") || strings.Contains(body, "refused") {
		t.Fatalf("internal error text leaked: %s", body)
	}
	if bidQty(e) != 0 {
		t.Fatal("an order was placed although the idempotency store failed")
	}
}

func TestIdempotency_ThirtyParallelIdenticalRequestsExecuteOnce(t *testing.T) {
	e := newIdemEnv(t, memory.NewIdempotencyStore(100))
	req := `{"client_order_id":"par-1",` + buy10[1:] + `}`
	const n = 30
	var wg sync.WaitGroup
	statuses := make([]int, n)
	bodies := make([]string, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			r, b := postWith(t, e, "TRADER", "", req)
			statuses[i], bodies[i] = r.StatusCode, b
		}(i)
	}
	close(start)
	wg.Wait()

	for i := range statuses {
		if statuses[i] != 201 {
			t.Fatalf("request %d: status %d (%s), want 201 for every caller", i, statuses[i], bodies[i])
		}
		if bodies[i] != bodies[0] {
			t.Fatalf("request %d got a different body", i)
		}
	}
	if got := bidQty(e); got != 10 {
		t.Fatalf("resting quantity = %d, want exactly one execution (10)", got)
	}
}
