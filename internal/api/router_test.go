package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/sorenhoang/go-observability-lab/internal/config"
)

func testRouter() http.Handler {
	return NewRouter(config.Config{})
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
