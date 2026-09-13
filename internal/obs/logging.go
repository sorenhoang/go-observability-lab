package obs

import (
	"context"
	"io"
	"log/slog"
)

// handler decorates a slog.Handler so every record picks up trace_id/span_id
// from the context automatically — call sites never remember to add them.
type handler struct {
	slog.Handler
}

// NewHandler builds a one-JSON-object-per-line slog.Handler at the given
// level, with trace/span IDs injected from ctx when a trace is active.
func NewHandler(w io.Writer, level slog.Leveler) slog.Handler {
	return handler{slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})}
}

func (h handler) Handle(ctx context.Context, rec slog.Record) error {
	if traceID := TraceIDFromContext(ctx); traceID != "" {
		rec.AddAttrs(
			slog.String("trace_id", traceID),
			slog.String("span_id", SpanIDFromContext(ctx)),
		)
	}
	return h.Handler.Handle(ctx, rec)
}

// WithAttrs/WithGroup must re-wrap, otherwise a derived logger (base.With(...),
// as RequestLogger uses) would drop back to the plain JSON handler and lose
// trace injection.
func (h handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return handler{h.Handler.WithAttrs(attrs)}
}

func (h handler) WithGroup(name string) slog.Handler {
	return handler{h.Handler.WithGroup(name)}
}
