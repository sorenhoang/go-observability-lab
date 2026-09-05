package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sorenhoang/go-observability-lab/internal/config"
	"github.com/sorenhoang/go-observability-lab/internal/metrics"
)

func testRouter() http.Handler {
	return NewRouter(config.Config{}, metrics.New())
}

func TestHealth(t *testing.T) {
	rec := doReq(t, http.MethodGet, "/health", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content-type = %q, want application/json", ct)
	}

	var body healthResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body.Status != "ok" {
		t.Fatalf("status field = %q, want ok", body.Status)
	}
}

func TestUnknownRoute404(t *testing.T) {
	rec := doReq(t, http.MethodGet, "/nope", "")

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestMetricsEndpointExposesHTTPMetrics(t *testing.T) {
	router := testRouter()

	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	router.ServeHTTP(httptest.NewRecorder(), req)

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"http_requests_total",
		"http_request_duration_seconds_bucket",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("/metrics missing %q", want)
		}
	}
}

func TestMetricsEndpointIsNotInstrumented(t *testing.T) {
	router := testRouter()

	for range 2 {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	}

	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if strings.Contains(rec.Body.String(), `route="/metrics"`) {
		t.Fatal("/metrics request was counted in application request metrics")
	}
}

func TestCreateOrderIncrementsBusinessMetric(t *testing.T) {
	router := testRouter()

	rec := httptest.NewRecorder()
	router.ServeHTTP(
		rec,
		httptest.NewRequest(http.MethodPost, "/orders", strings.NewReader(`{"product_id":1,"qty":1}`)),
	)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}

	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))

	if !strings.Contains(rec.Body.String(), "orders_created_total 1") {
		t.Fatal("orders_created_total did not increase after successful order")
	}
}
