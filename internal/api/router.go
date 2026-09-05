// Package api wires the HTTP routes for the lab service.
//
// Phase 1: the API exposes fake data and simulator endpoints. Later phases add
// /metrics and instrumentation middleware.
package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/sorenhoang/go-observability-lab/internal/config"
	"github.com/sorenhoang/go-observability-lab/internal/metrics"
)

// startedAt is captured at process start so /health can report uptime.
var startedAt = time.Now()

type Handlers struct {
	cfg      config.Config
	metrics  *metrics.Metrics
	orderSeq atomic.Int64
}

// NewRouter returns the HTTP handler for the service.
//
// It uses the standard library http.ServeMux with Go 1.22+ method/pattern
// routing (e.g. "GET /health"). No third-party router: the point of the lab
// is to keep the middleware seam visible, and ServeMux is enough.
func NewRouter(cfg config.Config, m *metrics.Metrics) http.Handler {
	h := &Handlers{cfg: cfg, metrics: m}
	instrument := m.Instrument(routePattern)

	mux := http.NewServeMux()
	mux.Handle("GET /health", instrument(http.HandlerFunc(handleHealth)))
	mux.Handle("GET /users", instrument(http.HandlerFunc(handleUsers)))
	mux.Handle("GET /products", instrument(http.HandlerFunc(handleProducts)))
	mux.Handle("POST /orders", instrument(http.HandlerFunc(h.handleCreateOrder)))
	mux.Handle("GET /slow", instrument(http.HandlerFunc(h.handleSlow)))
	mux.Handle("GET /error", instrument(http.HandlerFunc(h.handleError)))
	mux.Handle("GET /metrics", promhttp.HandlerFor(m.Registry(), promhttp.HandlerOpts{}))
	return mux
}

// routePattern returns the matched route template, or "other" when nothing
// matched. Phase 2 uses this to keep metric labels bounded.
func routePattern(r *http.Request) string {
	if r.Pattern == "" {
		return "other"
	}
	_, path, ok := strings.Cut(r.Pattern, " ")
	if !ok {
		return "other"
	}
	return path
}

type healthResponse struct {
	Status string `json:"status"`
	Uptime string `json:"uptime"`
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthResponse{
		Status: "ok",
		Uptime: time.Since(startedAt).Round(time.Second).String(),
	})
}

// writeJSON is the single JSON response helper used by every handler.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// Encode errors here are unrecoverable (client already got the status
	// line); log-and-move-on is the only sane option, and a bare _ keeps
	// the lab readable. Real services would log this.
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
