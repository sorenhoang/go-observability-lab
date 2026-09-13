package obs

import (
	"context"
	"log/slog"
)

// ctxKey is unexported so no other package can collide with these keys.
type ctxKey int

const (
	loggerCtxKey ctxKey = iota
	traceIDCtxKey
	spanIDCtxKey
)

// ContextWithLogger attaches a logger to ctx, typically one already carrying
// per-request attributes (route, request_id) via slog.Logger.With.
func ContextWithLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerCtxKey, l)
}

// LoggerFrom returns the logger stored on ctx, or slog.Default() when none
// was attached — callers never need to nil-check.
func LoggerFrom(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerCtxKey).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}

// contextWithTraceIDs stores the active trace/span IDs. Unexported: Phase 11's
// tracer middleware is the only writer, everything else only reads.
func contextWithTraceIDs(ctx context.Context, traceID, spanID string) context.Context {
	ctx = context.WithValue(ctx, traceIDCtxKey, traceID)
	ctx = context.WithValue(ctx, spanIDCtxKey, spanID)
	return ctx
}

// TraceIDFromContext returns "" when no trace is active on ctx.
func TraceIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(traceIDCtxKey).(string)
	return id
}

// SpanIDFromContext returns "" when no trace is active on ctx.
func SpanIDFromContext(ctx context.Context) string {
	id, _ := ctx.Value(spanIDCtxKey).(string)
	return id
}
