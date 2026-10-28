package kafka_test

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/kafka"
)

// TestLive_PublishAndSettleThroughRealBroker runs against a real Kafka/Redpanda
// and is skipped unless DDT_LIVE_KAFKA_BROKERS is set (for the local compose
// stack: DDT_LIVE_KAFKA_BROKERS=127.0.0.1:19092).
func TestLive_PublishAndSettleThroughRealBroker(t *testing.T) {
	brokers := os.Getenv("DDT_LIVE_KAFKA_BROKERS")
	if brokers == "" {
		t.Skip("DDT_LIVE_KAFKA_BROKERS not set")
	}
	cfg, err := kafka.ResolveBus(func(k string) string {
		if k == "KAFKA_BROKERS" {
			return brokers
		}
		return ""
	})
	if err != nil {
		t.Fatal(err)
	}
	bus, err := kafka.OpenBus(cfg, quiet())
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()

	group := fmt.Sprintf("ddt-live-test-%d", time.Now().UnixNano())
	sub, err := bus.Subscriber(group)
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeRepo{}
	c := kafka.NewSettlementConsumer(sub, repo, quiet()).WithBatching(50, 50*time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.Start(ctx) }()

	pub := kafka.NewWatermillPublisher(bus.Publisher(), quiet())
	defer pub.Close()
	id := "live-" + strings.ReplaceAll(group, "ddt-live-test-", "")
	if err := pub.PublishOrderMatched(ctx, goodEvent(id)); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(60 * time.Second)
	for repo.numSaved() == 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	cancel()
	<-done
	if repo.numSaved() == 0 {
		t.Fatalf("no trade settled through the broker within 60s (dead letters: %d)", pub.DeadLetterTotal())
	}
}
