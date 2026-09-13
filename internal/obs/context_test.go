package obs

import (
	"context"
	"log/slog"
	"testing"
)

// RED PHASE (Task 1). These fail to build until internal/obs/context.go exists.
// Expected red signal: `go test ./internal/obs/...` -> build failed -> undefined:
// LoggerFrom / ContextWithLogger / TraceIDFromContext / SpanIDFromContext /
// contextWithTraceIDs.

func TestLoggerFromReturnsDefaultWhenAbsent(t *testing.T) {
	if got := LoggerFrom(context.Background()); got == nil {
		t.Fatal("LoggerFrom(empty ctx) = nil, want a non-nil fallback (slog.Default())")
	}
}

func TestContextWithLoggerRoundTrips(t *testing.T) {
	want := slog.New(slog.DiscardHandler)
	ctx := ContextWithLogger(context.Background(), want)
	if got := LoggerFrom(ctx); got != want {
		t.Fatalf("LoggerFrom returned a different logger than was stored")
	}
}

func TestTraceHelpersEmptyWhenAbsent(t *testing.T) {
	if id := TraceIDFromContext(context.Background()); id != "" {
		t.Fatalf("TraceIDFromContext(empty) = %q, want empty string", id)
	}
	if id := SpanIDFromContext(context.Background()); id != "" {
		t.Fatalf("SpanIDFromContext(empty) = %q, want empty string", id)
	}
}

func TestTraceHelpersReadStoredValues(t *testing.T) {
	const traceID = "0af7651916cd43dd8448eb211c80319c"
	const spanID = "b7ad6b7169203331"

	ctx := contextWithTraceIDs(context.Background(), traceID, spanID)

	if got := TraceIDFromContext(ctx); got != traceID {
		t.Fatalf("TraceIDFromContext = %q, want %q", got, traceID)
	}
	if got := SpanIDFromContext(ctx); got != spanID {
		t.Fatalf("SpanIDFromContext = %q, want %q", got, spanID)
	}
}
