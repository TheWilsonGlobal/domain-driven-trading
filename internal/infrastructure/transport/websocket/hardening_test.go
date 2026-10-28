package websocket_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	wsTransport "github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/transport/websocket"
)

func newHardHub(t *testing.T, opt wsTransport.Options) (*wsTransport.MarketHub, string) {
	t.Helper()
	hub := wsTransport.NewMarketHub(quiet()).Configure(opt)
	go hub.Run()
	srv := httptest.NewServer(httpHandlerFunc(hub))
	t.Cleanup(srv.Close)
	return hub, "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws/market"
}

func dial(url, origin string) (*websocket.Conn, *http.Response, error) {
	h := http.Header{}
	if origin != "" {
		h.Set("Origin", origin)
	}
	return websocket.DefaultDialer.Dial(url, h)
}

func TestWS_OriginAllowlist(t *testing.T) {
	_, url := newHardHub(t, wsTransport.Options{AllowedOrigins: []string{"http://localhost:5471"}})

	for _, bad := range []string{"http://evil.example", "http://localhost:5471.evil.example", "http://localhost:9999", "null"} {
		c, resp, err := dial(url, bad)
		if err == nil {
			c.Close()
			t.Fatalf("origin %q was accepted", bad)
		}
		if resp == nil || resp.StatusCode != http.StatusForbidden {
			t.Fatalf("origin %q: response = %v, want 403", bad, resp)
		}
	}
	c, _, err := dial(url, "http://localhost:5471")
	if err != nil {
		t.Fatalf("allowed origin refused: %v", err)
	}
	c.Close()
	// Non-browser clients send no Origin and are not a CSWSH vector.
	c, _, err = dial(url, "")
	if err != nil {
		t.Fatalf("origin-less client refused: %v", err)
	}
	c.Close()
}

func TestWS_ClientCapIs503(t *testing.T) {
	hub, url := newHardHub(t, wsTransport.Options{MaxClients: 2})
	a, _, err := dial(url, "")
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := dial(url, "")
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()

	c, resp, err := dial(url, "")
	if err == nil {
		c.Close()
		t.Fatal("third client accepted over the cap")
	}
	if resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("over-cap response = %v, want 503", resp)
	}

	// A slot frees up when a client leaves.
	a.Close()
	deadline := time.Now().Add(2 * time.Second)
	for hub.ClientCount() > 1 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	d, _, err := dial(url, "")
	if err != nil {
		t.Fatalf("slot not released: %v", err)
	}
	d.Close()
}

func TestWS_ServerPingsAndDropsClientsThatStopPonging(t *testing.T) {
	opt := wsTransport.Options{PingPeriod: 40 * time.Millisecond, PongWait: 300 * time.Millisecond}
	hub, url := newHardHub(t, opt)

	// Reading client: gorilla answers pings automatically while it reads.
	good, _, err := dial(url, "")
	if err != nil {
		t.Fatal(err)
	}
	defer good.Close()
	var pings int32
	good.SetPingHandler(func(s string) error {
		atomic.AddInt32(&pings, 1)
		return good.WriteControl(websocket.PongMessage, []byte(s), time.Now().Add(time.Second))
	})
	go func() {
		for {
			if _, _, err := good.ReadMessage(); err != nil {
				return
			}
		}
	}()

	// Silent client: never reads, so it never pongs.
	silent, _, err := dial(url, "")
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && (atomic.LoadInt32(&pings) == 0 || hub.ClientCount() != 1) {
		time.Sleep(10 * time.Millisecond)
	}
	if atomic.LoadInt32(&pings) == 0 {
		t.Fatal("server never sent a ping")
	}
	if n := hub.ClientCount(); n != 1 {
		t.Fatalf("clients = %d, want 1 (the silent client must be dropped after the pong deadline)", n)
	}
}
