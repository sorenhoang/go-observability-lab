// Package api wires the HTTP routes for the lab service.
//
// Phase 0: only GET /health exists. Later phases add /users, /products,
// /orders, /slow, /error, /metrics and the instrumentation middleware.
package api

import (
	"encoding/json"
	"net/http"
	"time"
)

// startedAt is captured at process start so /health can report uptime.
var startedAt = time.Now()

// NewRouter returns the HTTP handler for the service.
//
// It uses the standard library http.ServeMux with Go 1.22+ method/pattern
// routing (e.g. "GET /health"). No third-party router: the point of the lab
// is to keep the middleware seam visible, and ServeMux is enough.
func NewRouter() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", handleHealth)
	return mux
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
