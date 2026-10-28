package websocket

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	gws "github.com/gorilla/websocket"
)

func quietLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// A client whose connection is dead is dropped, and the removal must not race
// with concurrent readers such as ClientCount.
func TestMarketHub_BroadcastDropsDeadClients(t *testing.T) {
	hub := NewMarketHub(quietLogger())
	go hub.Run()

	up := gws.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	serverConns := make(chan *gws.Conn, 64)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		serverConns <- c
	}))
	defer srv.Close()
	url := "ws" + strings.TrimPrefix(srv.URL, "http")

	const n = 20
	for i := 0; i < n; i++ {
		cc, _, err := gws.DefaultDialer.Dial(url, nil)
		if err != nil {
			t.Fatal(err)
		}
		defer cc.Close()
		sc := <-serverConns
		_ = sc.Close() // closed locally: every write on it fails deterministically
		hub.addClient(sc)
	}

	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				_ = hub.ClientCount()
			}
		}
	}()

	hub.BroadcastDepth(order.MarketDepthEvent{Symbol: "APX"})
	deadline := time.Now().Add(2 * time.Second)
	for hub.ClientCount() > 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	close(stop)
	wg.Wait()
	if got := hub.ClientCount(); got != 0 {
		t.Fatalf("dead clients were not removed, %d left", got)
	}
}

// A client that never drains its send buffer must not stall the hub or the
// other clients: its messages are dropped, the rest keep flowing.
func TestMarketHub_SlowClientDoesNotBlockOthers(t *testing.T) {
	hub := NewMarketHub(quietLogger()).Configure(Options{AllowedOrigins: []string{"http://localhost:5471"}})
	go hub.Run()

	slow := &client{send: make(chan []byte, 1)} // no pump: nothing ever drains it
	hub.mu.Lock()
	hub.clients[slow] = struct{}{}
	hub.mu.Unlock()

	srv := httptest.NewServer(http.HandlerFunc(hub.HandleWS))
	defer srv.Close()
	fast, _, err := gws.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer fast.Close()

	const total = 300
	got := make(chan int, 1)
	go func() {
		n := 0
		_ = fast.SetReadDeadline(time.Now().Add(5 * time.Second))
		for n < total {
			if _, _, err := fast.ReadMessage(); err != nil {
				break
			}
			n++
		}
		got <- n
	}()

	start := time.Now()
	for i := 0; i < total; i++ {
		hub.BroadcastDepth(order.MarketDepthEvent{Symbol: "APX"})
		time.Sleep(200 * time.Microsecond) // stay inside the 1024 broadcast buffer
	}
	select {
	case n := <-got:
		if n != total {
			t.Fatalf("fast client received %d of %d messages (took %s)", n, total, time.Since(start))
		}
	case <-time.After(6 * time.Second):
		t.Fatal("fast client starved by the slow one")
	}
	if hub.Dropped() == 0 {
		t.Fatal("expected drops for the slow client")
	}
}
