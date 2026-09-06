package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sorenhoang/go-observability-lab/internal/config"
	"github.com/sorenhoang/go-observability-lab/internal/metrics"
)

func chaosReq(t *testing.T, router http.Handler, method, target, body, token string) *httptest.ResponseRecorder {
	t.Helper()

	var r *strings.Reader
	if body == "" {
		r = strings.NewReader("")
	} else {
		r = strings.NewReader(body)
	}

	req := httptest.NewRequest(method, target, r)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestChaosDisabledPassesThroughBusinessRoutes(t *testing.T) {
	router := NewRouter(config.Config{}, metrics.New())

	rec := chaosReq(t, router, http.MethodGet, "/users", "", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestChaosErrorRatioOneInjects500(t *testing.T) {
	router := NewRouter(config.Config{}, metrics.New())
	chaosReq(t, router, http.MethodPost, "/admin/chaos", `{"enabled":true,"error_ratio":1}`, "")

	rec := chaosReq(t, router, http.MethodGet, "/users", "", "")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(rec.Body.String(), "chaos: injected failure") {
		t.Fatalf("body = %q, want injected failure", rec.Body.String())
	}
}

func TestChaosLatencyAddsDelay(t *testing.T) {
	router := NewRouter(config.Config{}, metrics.New())
	chaosReq(t, router, http.MethodPost, "/admin/chaos", `{"enabled":true,"latency_ms":25}`, "")

	start := time.Now()
	rec := chaosReq(t, router, http.MethodGet, "/users", "", "")
	elapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if elapsed < 20*time.Millisecond {
		t.Fatalf("elapsed = %s, want at least 20ms", elapsed)
	}
}

func TestChaosSetClampsValues(t *testing.T) {
	router := NewRouter(config.Config{}, metrics.New())

	rec := chaosReq(t, router, http.MethodPost, "/admin/chaos", `{"enabled":true,"latency_ms":999999,"error_ratio":2}`, "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	var got chaosState
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.LatencyMs != chaosMaxLatencyMs {
		t.Fatalf("latency_ms = %d, want %d", got.LatencyMs, chaosMaxLatencyMs)
	}
	if got.ErrorRatio != 1 {
		t.Fatalf("error_ratio = %v, want 1", got.ErrorRatio)
	}
}

func TestControlRoutesAreNotChaosAffected(t *testing.T) {
	router := NewRouter(config.Config{}, metrics.New())
	chaosReq(t, router, http.MethodPost, "/admin/chaos", `{"enabled":true,"error_ratio":1}`, "")

	rec := chaosReq(t, router, http.MethodGet, "/admin/chaos", "", "")

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
}

func TestAdminTokenRequiredWhenConfigured(t *testing.T) {
	router := NewRouter(config.Config{AdminToken: "secret"}, metrics.New())

	unauthorized := chaosReq(t, router, http.MethodGet, "/admin/chaos", "", "")
	if unauthorized.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status = %d, want %d", unauthorized.Code, http.StatusUnauthorized)
	}

	req := httptest.NewRequest(http.MethodGet, "/admin/chaos", nil)
	req.Header.Set("Authorization", "secret")
	rawHeader := httptest.NewRecorder()
	router.ServeHTTP(rawHeader, req)
	if rawHeader.Code != http.StatusUnauthorized {
		t.Fatalf("raw header status = %d, want %d", rawHeader.Code, http.StatusUnauthorized)
	}

	queryToken := chaosReq(t, router, http.MethodGet, "/admin/chaos?token=secret", "", "")
	if queryToken.Code != http.StatusOK {
		t.Fatalf("query token status = %d, want %d", queryToken.Code, http.StatusOK)
	}

	bearer := chaosReq(t, router, http.MethodGet, "/admin/chaos", "", "secret")
	if bearer.Code != http.StatusOK {
		t.Fatalf("bearer status = %d, want %d", bearer.Code, http.StatusOK)
	}
}

func TestClampAtoiCapsCPUSeconds(t *testing.T) {
	got := clampAtoi("999", 5, 0, cpuMaxSeconds)

	if got != cpuMaxSeconds {
		t.Fatalf("clamped seconds = %d, want %d", got, cpuMaxSeconds)
	}
}

func TestLeakGrowAndReset(t *testing.T) {
	router := NewRouter(config.Config{}, metrics.New())
	chaosReq(t, router, http.MethodPost, "/leak/reset", "", "")

	grow := chaosReq(t, router, http.MethodGet, "/leak?mb=1", "", "")
	if grow.Code != http.StatusOK {
		t.Fatalf("grow status = %d, want %d", grow.Code, http.StatusOK)
	}
	var grown map[string]int
	if err := json.NewDecoder(grow.Body).Decode(&grown); err != nil {
		t.Fatalf("decode grow response: %v", err)
	}
	if grown["leaked_mb"] < 1 {
		t.Fatalf("leaked_mb after grow = %d, want at least 1", grown["leaked_mb"])
	}

	reset := chaosReq(t, router, http.MethodPost, "/leak/reset", "", "")
	if reset.Code != http.StatusOK {
		t.Fatalf("reset status = %d, want %d", reset.Code, http.StatusOK)
	}
	var cleared map[string]int
	if err := json.NewDecoder(reset.Body).Decode(&cleared); err != nil {
		t.Fatalf("decode reset response: %v", err)
	}
	if cleared["leaked_mb"] != 0 {
		t.Fatalf("leaked_mb after reset = %d, want 0", cleared["leaked_mb"])
	}
}
