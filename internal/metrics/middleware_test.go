package metrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func serveInstrumented(m *Metrics, route string, handler http.HandlerFunc) {
	req := httptest.NewRequest(http.MethodGet, "/raw?x=1", nil)
	rec := httptest.NewRecorder()

	m.Instrument(func(*http.Request) string {
		return route
	})(handler).ServeHTTP(rec, req)
}

func TestInstrumentRecordsSuccessfulRequest(t *testing.T) {
	m := New()

	serveInstrumented(m, "/test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	if got := testutil.ToFloat64(m.requestsTotal.WithLabelValues("GET", "/test", "200")); got != 1 {
		t.Fatalf("requests_total = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.requestsInProgress.WithLabelValues("/test")); got != 0 {
		t.Fatalf("requests_in_progress = %v, want 0", got)
	}
	if got := testutil.CollectAndCount(m.requestDuration, "http_request_duration_seconds"); got == 0 {
		t.Fatal("duration histogram was not collected")
	}
	if got := testutil.CollectAndCount(m.requestDurationSummary, "http_request_duration_summary_seconds"); got == 0 {
		t.Fatal("duration summary was not collected")
	}
}

func TestInstrumentRecordsErrorStatus(t *testing.T) {
	m := New()

	serveInstrumented(m, "/test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})

	if got := testutil.ToFloat64(m.requestsTotal.WithLabelValues("GET", "/test", "500")); got != 1 {
		t.Fatalf("requests_total = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.requestsInProgress.WithLabelValues("/test")); got != 0 {
		t.Fatalf("requests_in_progress = %v, want 0", got)
	}
}

func TestInstrumentRecordsPanicAs500AndRepanics(t *testing.T) {
	m := New()

	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("middleware did not re-panic")
			}
		}()

		serveInstrumented(m, "/panic", func(http.ResponseWriter, *http.Request) {
			panic("boom")
		})
	}()

	if got := testutil.ToFloat64(m.requestsTotal.WithLabelValues("GET", "/panic", "500")); got != 1 {
		t.Fatalf("requests_total = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.requestsInProgress.WithLabelValues("/panic")); got != 0 {
		t.Fatalf("requests_in_progress = %v, want 0", got)
	}
}

func TestInstrumentUsesRouteFuncTemplateLabel(t *testing.T) {
	m := New()
	req := httptest.NewRequest(http.MethodPost, "/orders?x=1", nil)
	rec := httptest.NewRecorder()

	m.Instrument(func(*http.Request) string {
		return "/orders"
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	})).ServeHTTP(rec, req)

	if got := testutil.ToFloat64(m.requestsTotal.WithLabelValues("POST", "/orders", "201")); got != 1 {
		t.Fatalf("template route series = %v, want 1", got)
	}
	if got := testutil.ToFloat64(m.requestsTotal.WithLabelValues("POST", "/orders?x=1", "201")); got != 0 {
		t.Fatalf("raw route series = %v, want 0", got)
	}
}

// RED PHASE (Task 6). Fails until Instrument attaches an exemplar for a
// sampled span, served in OpenMetrics format (the only format that carries
// exemplars).
func TestDurationHistogramCarriesExemplarForSampledRequest(t *testing.T) {
	m := New()

	ctx, traceID := sampledCtx(t)
	req := httptest.NewRequest(http.MethodGet, "/orders", nil).WithContext(ctx)
	rec := httptest.NewRecorder()

	m.Instrument(func(*http.Request) string {
		return "/orders"
	})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(rec, req)

	metricsReq := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	metricsReq.Header.Set("Accept", "application/openmetrics-text")
	metricsRec := httptest.NewRecorder()
	promhttp.HandlerFor(m.Registry(), promhttp.HandlerOpts{EnableOpenMetrics: true}).ServeHTTP(metricsRec, metricsReq)

	body := metricsRec.Body.String()
	if !strings.Contains(body, `trace_id="`+traceID+`"`) {
		t.Fatalf("no exemplar for trace_id=%s on http_request_duration_seconds_bucket:\n%s", traceID, body)
	}
}

func TestOrderCreatedIncrementsBusinessMetric(t *testing.T) {
	m := New()

	m.OrderCreated()

	if got := testutil.ToFloat64(m.ordersCreated); got != 1 {
		t.Fatalf("orders_created_total = %v, want 1", got)
	}
}

func TestRegistryExposesCollectors(t *testing.T) {
	m := New()
	serveInstrumented(m, "/test", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	m.OrderCreated()

	names := map[string]bool{}
	metricFamilies, err := m.Registry().Gather()
	if err != nil {
		t.Fatalf("gather registry: %v", err)
	}
	for _, mf := range metricFamilies {
		names[mf.GetName()] = true
	}

	for _, name := range []string{
		"go_goroutines",
		"process_cpu_seconds_total",
		"http_requests_total",
		"http_request_duration_seconds",
		"http_request_duration_summary_seconds",
		"http_requests_in_progress",
		"orders_created_total",
	} {
		if !names[name] {
			t.Fatalf("registry missing %s", name)
		}
	}
}
