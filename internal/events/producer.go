// Package events publishes order events to Kafka. Publishing is
// best-effort: a failure here never fails the request that already
// committed the order to Postgres, the source of truth.
package events

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/segmentio/kafka-go"
	"github.com/sorenhoang/go-observability-lab/internal/metrics"
	"github.com/sorenhoang/go-observability-lab/internal/obs"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const OrdersTopic = "orders"

type OrderEvent struct {
	OrderID   int64     `json:"order_id"`
	ProductID int       `json:"product_id"`
	Qty       int       `json:"qty"`
	TS        time.Time `json:"ts"`
}

// kafkaWriter is the seam over *kafka.Writer — it lets tests inject a fake
// instead of dialing real Kafka, without changing PublishOrder's signature.
type kafkaWriter interface {
	WriteMessages(ctx context.Context, msgs ...kafka.Message) error
	Close() error
}

type Producer struct {
	writer  kafkaWriter
	metrics *metrics.Metrics
	enabled bool
}

func NewProducer(brokers string, m *metrics.Metrics) *Producer {
	addrs := SplitBrokers(brokers)
	if len(addrs) == 0 {
		return &Producer{metrics: m}
	}
	return newProducerWithWriter(&kafka.Writer{
		Addr:         kafka.TCP(addrs...),
		Topic:        OrdersTopic,
		BatchTimeout: 10 * time.Millisecond,
		RequiredAcks: kafka.RequireOne,
	}, m)
}

// newProducerWithWriter is the seam tests use to inject a fake kafkaWriter.
func newProducerWithWriter(w kafkaWriter, m *metrics.Metrics) *Producer {
	return &Producer{writer: w, metrics: m, enabled: true}
}

func NewProducerIfAvailable(ctx context.Context, brokers string, m *metrics.Metrics) *Producer {
	addrs := SplitBrokers(brokers)
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
	log := obs.LoggerFrom(ctx)

	b, err := json.Marshal(event)
	if err != nil {
		p.metrics.OrderPublishError()
		log.Warn("marshal order event failed", "err", err)
		return
	}

	// The span is created here, synchronously, on the request goroutine —
	// never inside go func() below. By the time that goroutine runs, the
	// request's own span may already have ended, so a span started there
	// would have no valid parent (an orphan, disconnected from the trace).
	// It ends inside the goroutine, once the actual publish completes.
	ctx, span := obs.Tracer().Start(ctx, "kafka.publish orders", trace.WithSpanKind(trace.SpanKindProducer))
	traceparent := obs.FormatTraceparent(span.SpanContext())

	go func() {
		defer span.End()
		writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
		defer cancel()
		err := p.writer.WriteMessages(writeCtx, kafka.Message{
			Key:   []byte(time.Now().Format(time.RFC3339Nano)),
			Value: b,
			Headers: []kafka.Header{
				{Key: "traceparent", Value: []byte(traceparent)},
			},
		})
		if err != nil {
			p.metrics.OrderPublishError()
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			log.Warn("publish order event failed", "err", err)
			return
		}
		p.metrics.OrderPublished()
	}()
}

// SplitBrokers parses a comma-separated broker list, trimming whitespace and
// dropping empty entries. Exported so cmd/consumer parses its own
// CONSUMER_KAFKA_BROKERS the same way instead of a second copy of this.
func SplitBrokers(brokers string) []string {
	var out []string
	for _, broker := range strings.Split(brokers, ",") {
		broker = strings.TrimSpace(broker)
		if broker != "" {
			out = append(out, broker)
		}
	}
	return out
}
