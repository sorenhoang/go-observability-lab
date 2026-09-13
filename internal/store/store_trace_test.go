package store

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/sorenhoang/go-observability-lab/internal/metrics"
)

// RED PHASE (Task 5). Fails until timed() creates a child span.

func TestTimedEmitsChildSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))

	s := &Store{metrics: metrics.New()} // db unused: fn is a stub
	_ = s.timed(context.Background(), "products.list", func(context.Context) error { return nil })

	if len(rec.Ended()) != 1 {
		t.Fatalf("spans = %d, want 1", len(rec.Ended()))
	}
	got := rec.Ended()[0]
	if got.Name() != "db.products.list" {
		t.Fatalf("span name = %q, want db.products.list", got.Name())
	}
	attrs := map[string]string{}
	for _, kv := range got.Attributes() {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	if attrs["db.operation"] != "products.list" || attrs["db.system"] != "postgresql" {
		t.Fatalf("attrs = %v", attrs)
	}
}

func TestTimedRecordsErrorOnSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))

	s := &Store{metrics: metrics.New()}
	_ = s.timed(context.Background(), "orders.insert", func(context.Context) error {
		return context.DeadlineExceeded
	})
	if rec.Ended()[0].Status().Code.String() != "Error" {
		t.Fatalf("span status not Error on a failing query")
	}
}
