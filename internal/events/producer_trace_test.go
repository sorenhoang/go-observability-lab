package events

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/segmentio/kafka-go"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	otelTrace "go.opentelemetry.io/otel/trace"

	"github.com/sorenhoang/go-observability-lab/internal/metrics"
	"github.com/sorenhoang/go-observability-lab/internal/obs"
)

// fakeWriter captures messages instead of hitting a real Kafka broker.
type fakeWriter struct {
	mu       sync.Mutex
	messages []kafka.Message
}

func (w *fakeWriter) WriteMessages(_ context.Context, msgs ...kafka.Message) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.messages = append(w.messages, msgs...)
	return nil
}

func (w *fakeWriter) Close() error { return nil }

// lastWrittenHeader returns the value of the named header on the most
// recently written message, failing the test if none was written.
func lastWrittenHeader(t *testing.T, p *Producer, key string) string {
	t.Helper()
	fw, ok := p.writer.(*fakeWriter)
	if !ok {
		t.Fatalf("producer writer is %T, want *fakeWriter", p.writer)
	}
	fw.mu.Lock()
	defer fw.mu.Unlock()
	if len(fw.messages) == 0 {
		t.Fatal("no message written")
	}
	last := fw.messages[len(fw.messages)-1]
	for _, h := range last.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	t.Fatalf("message has no %q header", key)
	return ""
}

// ParseTraceparentTest is a thin shim over obs.ParseTraceparent so this
// test doesn't need to import obs's internal test helpers directly.
func ParseTraceparentTest(v string) (otelTrace.SpanContext, bool) {
	return obs.ParseTraceparent(v)
}

// RED PHASE (Task 5). Fails to build until producer.go provides
// newProducerWithWriter/fakeWriter/lastWrittenHeader/ParseTraceparentTest and
// PublishOrder starts a span synchronously before the publish goroutine.

func TestPublishOrderInjectsTraceparentAndEndsSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))

	// A fake writer capturing headers instead of hitting Kafka.
	p := newProducerWithWriter(&fakeWriter{}, metrics.New()) // helpers added by the implementer

	ctx, root := otel.Tracer("test").Start(context.Background(), "root")
	p.PublishOrder(ctx, OrderEvent{OrderID: 1, ProductID: 1, Qty: 1, TS: time.Now()})
	root.End()

	// The async publish + span end race: poll briefly.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) && len(rec.Ended()) < 2 {
		time.Sleep(5 * time.Millisecond)
	}

	var publishSpan sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == "kafka.publish orders" {
			publishSpan = s
		}
	}
	if publishSpan == nil {
		t.Fatal("no kafka.publish orders span ended")
	}
	if publishSpan.Parent().TraceID() != root.SpanContext().TraceID() {
		t.Fatal("publish span is not a child of the request span")
	}
	hdr := lastWrittenHeader(t, p, "traceparent")
	if _, ok := ParseTraceparentTest(hdr); !ok { // tiny test shim over obs.ParseTraceparent
		t.Fatalf("traceparent header not valid: %q", hdr)
	}
}
