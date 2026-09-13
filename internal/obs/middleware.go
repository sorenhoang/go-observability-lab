package obs

import (
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"
)

// responseRecorder captures the final status and bytes written, mirroring
// metrics.statusRecorder's shim so both middlewares behave the same way when
// composed with metrics.Instrument in the router.
type responseRecorder struct {
	http.ResponseWriter
	status      int
	bytes       int64
	wroteHeader bool
}

func (r *responseRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *responseRecorder) Write(b []byte) (int, error) {
	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	return n, err
}

func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// PanicGuard recovers a handler panic, logs it via base (not a ctx-derived
// logger — this must work standalone, with nothing on the context yet), and
// writes a clean 500 if the handler hadn't written a header already. It never
// re-panics, so it must sit inside metrics.Instrument to keep that
// middleware's own recover+re-panic from firing on every request.
func PanicGuard(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			defer func() {
				rp := recover()
				if rp == nil {
					return
				}
				base.LogAttrs(r.Context(), slog.LevelError, "panic recovered",
					slog.Any("panic", rp),
					slog.String("stack", string(debug.Stack())),
				)
				if !rec.wroteHeader {
					rec.WriteHeader(http.StatusInternalServerError)
				}
			}()
			next.ServeHTTP(rec, r)
		})
	}
}

// RequestLogger mints a request_id, attaches a request-scoped logger to the
// context, and emits exactly one canonical "request" line after the handler
// returns. It never reads the body, headers, or query string — those can
// carry secrets (tokens, PII) that must never reach log storage.
func RequestLogger(base *slog.Logger, routeFunc func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger := base.With(
				"method", r.Method,
				"route", routeFunc(r),
				"request_id", newRequestID(),
			)
			ctx := ContextWithLogger(r.Context(), logger)

			rec := &responseRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()

			next.ServeHTTP(rec, r.WithContext(ctx))

			level := slog.LevelInfo
			switch {
			case rec.status >= http.StatusInternalServerError:
				level = slog.LevelError
			case rec.status >= http.StatusBadRequest:
				level = slog.LevelWarn
			}

			logger.LogAttrs(ctx, level, "request",
				slog.Int("status", rec.status),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				slog.Int("bytes_out", int(rec.bytes)),
			)
		})
	}
}

// TraceHTTP is a pass-through stub in Phase 9 — Task 5 (Phase 11) replaces the
// body with real span creation. It exists now so the router can settle on its
// final middleware order without a second reorder later.
func TraceHTTP(routeFunc func(*http.Request) string) func(http.Handler) http.Handler {
	_ = routeFunc
	return func(next http.Handler) http.Handler {
		return next
	}
}

func newRequestID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
