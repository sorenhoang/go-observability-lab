package obs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// RED PHASE (Task 4). Fails to build / span-count assertions fail until
// internal/obs/middleware.go's TraceHTTP stops being a no-op.

func recordingProvider(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec)))
	return rec
}

func TestTraceHTTPStartsServerSpanWithAttrs(t *testing.T) {
	rec := recordingProvider(t)
	h := TraceHTTP(func(*http.Request) string { return "/orders" })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(201) }))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/orders", nil))

	if len(rec.Ended()) != 1 {
		t.Fatalf("spans = %d, want 1", len(rec.Ended()))
	}
	s := rec.Ended()[0]
	if s.Name() != "/orders" {
		t.Fatalf("span name = %q, want /orders", s.Name())
	}
	attrs := map[string]string{}
	for _, kv := range s.Attributes() {
		attrs[string(kv.Key)] = kv.Value.Emit()
	}
	if attrs["http.request.method"] != "POST" || attrs["http.route"] != "/orders" {
		t.Fatalf("attrs = %v", attrs)
	}
	if attrs["http.response.status_code"] != "201" {
		t.Fatalf("status_code attr = %q", attrs["http.response.status_code"])
	}
}

func TestTraceHTTPPutsTraceIDOnContext(t *testing.T) {
	recordingProvider(t)
	var seen string
	h := TraceHTTP(func(*http.Request) string { return "/x" })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = TraceIDFromContext(r.Context())
		}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))
	if len(seen) != 32 {
		t.Fatalf("TraceIDFromContext in handler = %q (len %d), want a 32-hex id", seen, len(seen))
	}
}

func TestTraceHTTPMarksErrorOn5xx(t *testing.T) {
	rec := recordingProvider(t)
	h := TraceHTTP(func(*http.Request) string { return "/x" })(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	if got := rec.Ended()[0].Status().Code.String(); got != "Error" {
		t.Fatalf("span status = %s, want Error", got)
	}
}

var _ = context.Background
