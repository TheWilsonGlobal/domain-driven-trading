package kafka

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/IBM/sarama"
	"github.com/ThreeDotsLabs/watermill"
	wmkafka "github.com/ThreeDotsLabs/watermill-kafka/v3/pkg/kafka"
	"github.com/ThreeDotsLabs/watermill/message"
	"github.com/ThreeDotsLabs/watermill/pubsub/gochannel"
)

// BusMode says which event bus implementation is in use (decision D5).
type BusMode string

const (
	BusKafka  BusMode = "kafka"
	BusMemory BusMode = "memory"
)

// BusConfig is the resolved event bus configuration.
type BusConfig struct {
	Mode    BusMode
	Brokers []string
	// SASL/SCRAM-SHA-256 credentials and TLS, all off by default (local compose).
	SASLUser     string
	SASLPassword string
	TLS          bool
}

// ResolveBus decides, from the environment, which bus runs:
//
//	BUS=memory                -> in-memory (only when asked for; the caller logs it)
//	BUS=kafka | unset         -> Kafka/Redpanda; KAFKA_BROKERS (comma list) required
//	neither configured        -> error: there is no silent in-memory fallback
//
// KAFKA_SASL_USER + KAFKA_SASL_PASSWORD enable SASL/SCRAM-SHA-256, KAFKA_TLS=true
// enables TLS. Secrets are never put in an error message.
func ResolveBus(getenv func(string) string) (BusConfig, error) {
	brokersRaw := strings.TrimSpace(getenv("KAFKA_BROKERS"))
	switch v := strings.ToLower(strings.TrimSpace(getenv("BUS"))); v {
	case string(BusMemory):
		return BusConfig{Mode: BusMemory}, nil
	case "", string(BusKafka):
		if brokersRaw == "" {
			if v == "" {
				return BusConfig{}, errors.New("no event bus configured: set KAFKA_BROKERS (Kafka/Redpanda) or BUS=memory explicitly")
			}
			return BusConfig{}, errors.New("BUS=kafka requires KAFKA_BROKERS")
		}
	default:
		return BusConfig{}, fmt.Errorf("unknown BUS %q (want kafka or memory)", v)
	}

	var brokers []string
	for _, b := range strings.Split(brokersRaw, ",") {
		b = strings.TrimSpace(b)
		if b == "" {
			return BusConfig{}, errors.New("KAFKA_BROKERS has an empty entry (want host:port,host:port)")
		}
		brokers = append(brokers, b)
	}

	cfg := BusConfig{
		Mode:         BusKafka,
		Brokers:      brokers,
		SASLUser:     strings.TrimSpace(getenv("KAFKA_SASL_USER")),
		SASLPassword: getenv("KAFKA_SASL_PASSWORD"),
	}
	switch {
	case cfg.SASLUser != "" && cfg.SASLPassword == "":
		return BusConfig{}, errors.New("KAFKA_SASL_USER is set but KAFKA_SASL_PASSWORD is empty")
	case cfg.SASLUser == "" && cfg.SASLPassword != "":
		return BusConfig{}, errors.New("KAFKA_SASL_PASSWORD is set but KAFKA_SASL_USER is empty")
	}
	switch tlsRaw := strings.ToLower(strings.TrimSpace(getenv("KAFKA_TLS"))); tlsRaw {
	case "", "false", "0":
	case "true", "1":
		cfg.TLS = true
	default:
		return BusConfig{}, errors.New("KAFKA_TLS must be true or false")
	}
	return cfg, nil
}

// SaramaConfig is the client configuration shared by publisher and
// subscriber: short connect timeouts so a dead broker fails the start fast,
// plus the optional TLS / SASL settings.
func SaramaConfig(cfg BusConfig) *sarama.Config {
	sc := sarama.NewConfig()
	applyNet(sc, cfg)
	return sc
}

func applyNet(sc *sarama.Config, cfg BusConfig) {
	sc.Net.DialTimeout = 3 * time.Second
	sc.Metadata.Retry.Max = 1
	sc.Metadata.Retry.Backoff = 100 * time.Millisecond
	if cfg.TLS {
		sc.Net.TLS.Enable = true
		sc.Net.TLS.Config = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if cfg.SASLUser != "" {
		sc.Net.SASL.Enable = true
		sc.Net.SASL.Mechanism = sarama.SASLTypeSCRAMSHA256
		sc.Net.SASL.SCRAMClientGeneratorFunc = newSCRAMClient
		sc.Net.SASL.Handshake = true
		sc.Net.SASL.User = cfg.SASLUser
		sc.Net.SASL.Password = cfg.SASLPassword
	}
}

// Bus owns the publisher and hands out subscribers for one resolved config.
type Bus struct {
	cfg    BusConfig
	logger *slog.Logger
	pub    message.Publisher
	mem    *gochannel.GoChannel // memory mode: one channel is both sides

	mu   sync.Mutex
	subs []message.Subscriber
}

// OpenBus builds the bus. In Kafka mode the producer connects immediately, so
// an unreachable broker (or rejected credentials) is a start failure with a
// clear error; nothing degrades to memory.
func OpenBus(cfg BusConfig, logger *slog.Logger) (*Bus, error) {
	b := &Bus{cfg: cfg, logger: logger}
	switch cfg.Mode {
	case BusMemory:
		logger.Warn("BUS=memory: events stay inside this process; no other service (settlement worker) will receive them")
		b.mem = gochannel.NewGoChannel(gochannel.Config{OutputChannelBuffer: 1000}, watermill.NewSlogLogger(logger))
		b.pub = b.mem
		return b, nil
	case BusKafka:
		sc := wmkafka.DefaultSaramaSyncPublisherConfig()
		applyNet(sc, cfg)
		pub, err := wmkafka.NewPublisher(wmkafka.PublisherConfig{
			Brokers:               cfg.Brokers,
			Marshaler:             marshaler(),
			OverwriteSaramaConfig: sc,
		}, watermill.NewSlogLogger(logger))
		if err != nil {
			return nil, fmt.Errorf("Kafka brokers %s unreachable or refused the connection: %w", strings.Join(cfg.Brokers, ","), err)
		}
		b.pub = pub
		return b, nil
	default:
		return nil, fmt.Errorf("unknown bus mode %q", cfg.Mode)
	}
}

func marshaler() wmkafka.MarshalerUnmarshaler {
	return wmkafka.NewWithPartitioningMarshaler(func(_ string, msg *message.Message) (string, error) {
		return msg.Metadata.Get("partition_key"), nil
	})
}

// Publisher returns the raw watermill publisher.
func (b *Bus) Publisher() message.Publisher { return b.pub }

// Subscriber returns a subscriber. A non-empty group makes consumers share the
// topic (work queue; starts from the oldest offset for a new group so no trade
// is skipped). An empty group means "every instance sees every message"
// (broadcast, newest offset): used for the websocket feed.
func (b *Bus) Subscriber(group string) (message.Subscriber, error) {
	if b.cfg.Mode == BusMemory {
		return b.mem, nil
	}
	sc := wmkafka.DefaultSaramaSubscriberConfig()
	applyNet(sc, b.cfg)
	if group != "" {
		sc.Consumer.Offsets.Initial = sarama.OffsetOldest
	}
	sub, err := wmkafka.NewSubscriber(wmkafka.SubscriberConfig{
		Brokers:               b.cfg.Brokers,
		Unmarshaler:           marshaler(),
		ConsumerGroup:         group,
		OverwriteSaramaConfig: sc,
	}, watermill.NewSlogLogger(b.logger))
	if err != nil {
		return nil, fmt.Errorf("Kafka subscriber: %w", err)
	}
	b.mu.Lock()
	b.subs = append(b.subs, sub)
	b.mu.Unlock()
	return sub, nil
}

// Mode reports the resolved mode.
func (b *Bus) Mode() BusMode { return b.cfg.Mode }

// Close closes the subscribers and the publisher.
func (b *Bus) Close() error {
	b.mu.Lock()
	subs := b.subs
	b.subs = nil
	b.mu.Unlock()
	var errs []error
	for _, s := range subs {
		errs = append(errs, s.Close())
	}
	errs = append(errs, b.pub.Close())
	return errors.Join(errs...)
}
