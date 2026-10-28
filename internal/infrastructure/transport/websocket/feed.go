package websocket

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"

	"github.com/ThreeDotsLabs/watermill/message"

	"github.com/domain-driven-trading/domain-driven-trading/internal/app/ports"
	"github.com/domain-driven-trading/domain-driven-trading/internal/domain/order"
)

// Broadcaster is what the feed needs from the hub.
type Broadcaster interface {
	BroadcastDepth(order.MarketDepthEvent)
	BroadcastTrade(order.OrderMatchedEvent)
}

// Feed connects the event bus to the websocket hub: market.depth snapshots
// become MARKET_DEPTH messages and OrderMatched events on orders.events become
// TRADE_TICK messages. Other events and unparseable payloads are skipped (and
// acked); the feed is best-effort and never blocks the bus.
type Feed struct {
	sub    message.Subscriber
	hub    Broadcaster
	logger *slog.Logger
}

func NewFeed(sub message.Subscriber, hub Broadcaster, logger *slog.Logger) *Feed {
	return &Feed{sub: sub, hub: hub, logger: logger}
}

// Run subscribes to both topics, closes ready (if non-nil) once subscribed,
// and forwards until ctx is cancelled or a channel closes.
func (f *Feed) Run(ctx context.Context, ready chan<- struct{}) error {
	depth, err := f.sub.Subscribe(ctx, ports.TopicMarketDepth)
	if err != nil {
		return err
	}
	events, err := f.sub.Subscribe(ctx, ports.TopicOrderEvents)
	if err != nil {
		return err
	}
	if ready != nil {
		close(ready)
	}
	f.logger.Info("websocket feed subscribed", "topics", []string{ports.TopicMarketDepth, ports.TopicOrderEvents})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		f.pump(ctx, depth, f.onDepth)
	}()
	go func() {
		defer wg.Done()
		f.pump(ctx, events, f.onEvent)
	}()
	wg.Wait()
	return ctx.Err()
}

func (f *Feed) pump(ctx context.Context, ch <-chan *message.Message, fn func(*message.Message)) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			fn(msg)
			msg.Ack()
		}
	}
}

func (f *Feed) onDepth(msg *message.Message) {
	var ev order.MarketDepthEvent
	if err := json.Unmarshal(msg.Payload, &ev); err != nil {
		f.logger.Warn("feed: unparseable market depth event", "error", err)
		return
	}
	f.hub.BroadcastDepth(ev)
}

func (f *Feed) onEvent(msg *message.Message) {
	if msg.Metadata.Get("event_type") != "OrderMatched" {
		return
	}
	var ev order.OrderMatchedEvent
	if err := json.Unmarshal(msg.Payload, &ev); err != nil {
		f.logger.Warn("feed: unparseable trade event", "error", err)
		return
	}
	// Public market data: account ids, order ids and reservation amounts are
	// private to the matching/settlement side and must not reach every browser.
	f.hub.BroadcastTrade(order.OrderMatchedEvent{
		TradeID:   ev.TradeID,
		Symbol:    ev.Symbol,
		TakerSide: ev.TakerSide,
		Price:     ev.Price,
		Quantity:  ev.Quantity,
		Timestamp: ev.Timestamp,
	})
}
