package obs

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// RED PHASE (Task 4). Fails to build until internal/obs/tracing.go provides
// InitTracer / Tracer.

func TestInitTracerNoopOnEmptyEndpoint(t *testing.T) {
	shutdown, err := InitTracer(context.Background(), "test", "", 1.0)
	if err != nil {
		t.Fatalf("InitTracer with empty endpoint errored: %v", err)
	}
	if err := shutdown(context.Background()); err != nil {
		t.Fatalf("no-op shutdown errored: %v", err)
	}
	// Creating a span must not panic even with the no-op provider.
	_, span := Tracer().Start(context.Background(), "x")
	span.End()
}

func TestTracerRecordsSampledSpan(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))

	_, span := Tracer().Start(context.Background(), "unit")
	span.End()

	if n := len(rec.Ended()); n != 1 {
		t.Fatalf("recorded %d spans, want 1", n)
	}
	if rec.Ended()[0].Name() != "unit" {
		t.Fatalf("span name = %q", rec.Ended()[0].Name())
	}
}
