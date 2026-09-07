package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/sorenhoang/go-observability-lab/internal/metrics"
)

const OrdersTopic = "orders"

type OrderEvent struct {
	OrderID   int64     `json:"order_id"`
	ProductID int       `json:"product_id"`
	Qty       int       `json:"qty"`
	TS        time.Time `json:"ts"`
}

type Producer struct {
	writer  *kafka.Writer
	metrics *metrics.Metrics
	enabled bool
}

func NewProducer(brokers string, m *metrics.Metrics) *Producer {
	addrs := splitBrokers(brokers)
	if len(addrs) == 0 {
		return &Producer{metrics: m}
	}
	return &Producer{
		writer: &kafka.Writer{
			Addr:         kafka.TCP(addrs...),
			Topic:        OrdersTopic,
			BatchTimeout: 10 * time.Millisecond,
			RequiredAcks: kafka.RequireOne,
		},
		metrics: m,
		enabled: true,
	}
}

func NewProducerIfAvailable(ctx context.Context, brokers string, m *metrics.Metrics) *Producer {
	addrs := splitBrokers(brokers)
	if len(addrs) == 0 {
		slog.Warn("API_KAFKA_BROKERS is empty; order publishing disabled")
		return &Producer{metrics: m}
	}
	dialCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	conn, err := kafka.DialContext(dialCtx, "tcp", addrs[0])
	if err != nil {
		slog.Warn("kafka unavailable at startup; order publishing disabled", "err", err)
		return &Producer{metrics: m}
	}
	if err := conn.CreateTopics(kafka.TopicConfig{Topic: OrdersTopic, NumPartitions: 1, ReplicationFactor: 1}); err != nil {
		slog.Warn("kafka topic creation failed; order publishing may rely on auto-create", "err", err)
	}
	_ = conn.Close()
	return NewProducer(brokers, m)
}

func (p *Producer) Close() error {
	if p.writer == nil {
		return nil
	}
	return p.writer.Close()
}

func (p *Producer) PublishOrder(ctx context.Context, event OrderEvent) {
	if !p.enabled {
		p.metrics.OrderPublishError()
		return
	}
	b, err := json.Marshal(event)
	if err != nil {
		p.metrics.OrderPublishError()
		slog.Warn("marshal order event failed", "err", err)
		return
	}
	go func() {
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		err := p.writer.WriteMessages(writeCtx, kafka.Message{
			Key:   []byte(time.Now().Format(time.RFC3339Nano)),
			Value: b,
		})
		if err != nil {
			p.metrics.OrderPublishError()
			slog.Warn("publish order event failed", "err", err)
			return
		}
		p.metrics.OrderPublished()
	}()
}

func splitBrokers(brokers string) []string {
	var out []string
	for _, broker := range strings.Split(brokers, ",") {
		broker = strings.TrimSpace(broker)
		if broker != "" {
			out = append(out, broker)
		}
	}
	return out
}
