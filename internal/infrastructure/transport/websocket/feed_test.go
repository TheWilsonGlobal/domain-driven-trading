package websocket_test

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/command"
	"github.com/domain-driven-trading/domain-driven-trading/internal/app/seed"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/kafka"
	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/persistence/postgres"
	wsTransport "github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/transport/websocket"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// A crossing order must reach a connected websocket client through
// handler -> publisher -> bus -> feed -> hub within one second.
func TestFeed_CrossingOrderReachesWebsocketClientWithinOneSecond(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	bus, err := kafka.OpenBus(kafka.BusConfig{Mode: kafka.BusMemory}, quiet())
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	hub := wsTransport.NewMarketHub(quiet())
	go hub.Run()
	srv := httptest.NewServer(httpHandlerFunc(hub))
	defer srv.Close()

	sub, err := bus.Subscriber("")
	if err != nil {
		t.Fatal(err)
	}
	feed := wsTransport.NewFeed(sub, hub, quiet())
	ready, feedDone := make(chan struct{}), make(chan error, 1)
	go func() { feedDone <- feed.Run(ctx, ready) }()
	select {
	case <-ready:
	case <-time.After(2 * time.Second):
		t.Fatal("feed did not subscribe")
	}

	conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(srv.URL, "http")+"/ws/market", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for i := 0; i < 100 && hub.ClientCount() == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}

	book := order.NewOrderBook("APX")
	m := order.NewMatcher(book)
	accounts := postgres.NewPostgresAccountRepository(nil)
	if _, err := seed.Accounts(ctx, accounts, seed.Defaults(), quiet()); err != nil {
		t.Fatal(err)
	}
	pub := kafka.NewWatermillPublisher(bus.Publisher(), quiet())
	defer pub.Close()
	h := command.NewPlaceOrderHandler(m, accounts, pub)

	if _, err := h.Handle(ctx, command.PlaceOrderCommand{
		ClientOrderID: "s1", AccountID: "ACC_INST_MM", Symbol: "APX", Side: order.SideSell,
		Type: order.TypeLimit, Price: 19500, Quantity: 100,
	}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	if _, err := h.Handle(ctx, command.PlaceOrderCommand{
		ClientOrderID: "b1", AccountID: "ACC_APX_001", Symbol: "APX", Side: order.SideBuy,
		Type: order.TypeLimit, Price: 19500, Quantity: 10,
	}); err != nil {
		t.Fatal(err)
	}

	gotTrade, gotDepth := false, false
	_ = conn.SetReadDeadline(start.Add(time.Second))
	for !(gotTrade && gotDepth) {
		_, data, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("no hub message within 1s (trade=%v depth=%v): %v", gotTrade, gotDepth, err)
		}
		var env struct {
			Type string          `json:"type"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal(data, &env); err != nil {
			t.Fatal(err)
		}
		switch env.Type {
		case "TRADE_TICK":
			var ev order.OrderMatchedEvent
			if err := json.Unmarshal(env.Data, &ev); err != nil || ev.Quantity != 10 || ev.Price != 19500 {
				t.Fatalf("bad trade tick: %s (%v)", env.Data, err)
			}
			gotTrade = true
		case "MARKET_DEPTH":
			gotDepth = true
		}
	}
	cancel()
	select {
	case <-feedDone:
	case <-time.After(2 * time.Second):
		t.Fatal("feed did not stop on cancel")
	}
}
