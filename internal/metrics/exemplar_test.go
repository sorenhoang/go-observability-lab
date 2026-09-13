package metrics

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// RED PHASE (Task 6). Fails until exemplar.go provides exemplarFor.

func sampledCtx(t *testing.T) (context.Context, string) {
	t.Helper()
	tid, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	sid, _ := trace.SpanIDFromHex("b7ad6b7169203331")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled})
	return trace.ContextWithSpanContext(context.Background(), sc), tid.String()
}

func TestExemplarForSampledSpan(t *testing.T) {
	ctx, want := sampledCtx(t)
	got := exemplarFor(ctx)
	if got == nil || got["trace_id"] != want {
		t.Fatalf("exemplarFor(sampled) = %v, want trace_id=%s", got, want)
	}
}

func TestExemplarForUnsampledSpanIsNil(t *testing.T) {
	tid, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	sid, _ := trace.SpanIDFromHex("b7ad6b7169203331")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid}) // not sampled
	if got := exemplarFor(trace.ContextWithSpanContext(context.Background(), sc)); got != nil {
		t.Fatalf("exemplarFor(unsampled) = %v, want nil", got)
	}
}

func TestExemplarForNoSpanIsNil(t *testing.T) {
	if got := exemplarFor(context.Background()); got != nil {
		t.Fatalf("exemplarFor(no span) = %v, want nil", got)
	}
}
