package kafka_test

import (
	"io"
	"log/slog"
	"net"
	"strings"
	"testing"

	"github.com/domain-driven-trading/domain-driven-trading/internal/infrastructure/kafka"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestResolveBus_Selection(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		mode    kafka.BusMode
		brokers []string
		wantErr string
	}{
		{"brokers selects kafka", map[string]string{"KAFKA_BROKERS": "a:9092, b:9092"}, kafka.BusKafka, []string{"a:9092", "b:9092"}, ""},
		{"explicit kafka", map[string]string{"BUS": "kafka", "KAFKA_BROKERS": "a:9092"}, kafka.BusKafka, []string{"a:9092"}, ""},
		{"explicit memory", map[string]string{"BUS": "memory"}, kafka.BusMemory, nil, ""},
		{"memory wins over brokers when explicit", map[string]string{"BUS": "MEMORY", "KAFKA_BROKERS": "a:9092"}, kafka.BusMemory, nil, ""},
		{"nothing configured fails", map[string]string{}, "", nil, "no event bus configured"},
		{"kafka without brokers fails", map[string]string{"BUS": "kafka"}, "", nil, "requires KAFKA_BROKERS"},
		{"invalid BUS", map[string]string{"BUS": "rabbit", "KAFKA_BROKERS": "a:9092"}, "", nil, "unknown BUS"},
		{"blank broker entry", map[string]string{"KAFKA_BROKERS": "a:9092,,"}, "", nil, "KAFKA_BROKERS"},
		{"sasl user without password", map[string]string{"KAFKA_BROKERS": "a:9092", "KAFKA_SASL_USER": "u"}, "", nil, "KAFKA_SASL_PASSWORD"},
		{"sasl password without user", map[string]string{"KAFKA_BROKERS": "a:9092", "KAFKA_SASL_PASSWORD": "p"}, "", nil, "KAFKA_SASL_USER"},
		{"bad KAFKA_TLS", map[string]string{"KAFKA_BROKERS": "a:9092", "KAFKA_TLS": "maybe"}, "", nil, "KAFKA_TLS"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := kafka.ResolveBus(env(tc.env))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want containing %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if cfg.Mode != tc.mode {
				t.Fatalf("mode = %q, want %q", cfg.Mode, tc.mode)
			}
			if strings.Join(cfg.Brokers, ",") != strings.Join(tc.brokers, ",") {
				t.Fatalf("brokers = %v, want %v", cfg.Brokers, tc.brokers)
			}
		})
	}
}

func TestResolveBus_SecurityOptionsOffByDefault(t *testing.T) {
	cfg, err := kafka.ResolveBus(env(map[string]string{"KAFKA_BROKERS": "a:9092"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.TLS || cfg.SASLUser != "" || cfg.SASLPassword != "" {
		t.Fatalf("security options must be off by default: %+v", cfg)
	}
	sc := kafka.SaramaConfig(cfg)
	if sc.Net.TLS.Enable || sc.Net.SASL.Enable {
		t.Fatal("sarama TLS/SASL enabled by default")
	}

	cfg, err = kafka.ResolveBus(env(map[string]string{
		"KAFKA_BROKERS": "a:9092", "KAFKA_SASL_USER": "u", "KAFKA_SASL_PASSWORD": "p", "KAFKA_TLS": "true",
	}))
	if err != nil {
		t.Fatal(err)
	}
	sc = kafka.SaramaConfig(cfg)
	if !sc.Net.TLS.Enable || !sc.Net.SASL.Enable || sc.Net.SASL.User != "u" || sc.Net.SASL.Password != "p" {
		t.Fatalf("security options not applied: tls=%v sasl=%v", sc.Net.TLS.Enable, sc.Net.SASL.Enable)
	}
}

func TestOpenBus_MemoryWorksWithoutBroker(t *testing.T) {
	bus, err := kafka.OpenBus(kafka.BusConfig{Mode: kafka.BusMemory}, quiet())
	if err != nil {
		t.Fatal(err)
	}
	defer bus.Close()
	if bus.Publisher() == nil {
		t.Fatal("no publisher")
	}
	if _, err := bus.Subscriber("g"); err != nil {
		t.Fatal(err)
	}
}

func TestOpenBus_UnreachableBrokerFailsClearly(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close() // the port is now closed

	_, err = kafka.OpenBus(kafka.BusConfig{
		Mode: kafka.BusKafka, Brokers: []string{addr}, SASLUser: "u", SASLPassword: "hunter2-secret",
	}, quiet())
	if err == nil {
		t.Fatal("expected an error for an unreachable broker")
	}
	if !strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("error not clear: %v", err)
	}
	if strings.Contains(err.Error(), "hunter2-secret") {
		t.Fatal("password leaked into the error")
	}
}
