package main

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebhookHandlerLogsAlerts(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	handler := newWebhookHandler(logger)

	body := `{
		"status":"firing",
		"alerts":[{
			"status":"firing",
			"labels":{"alertname":"HighErrorRate","severity":"warning"},
			"annotations":{"summary":"5xx error ratio above 5%"}
		}]
	}`
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	got := logs.String()
	for _, want := range []string{"msg=alert", "name=HighErrorRate", "severity=warning", "summary=\"5xx error ratio above 5%\""} {
		if !strings.Contains(got, want) {
			t.Fatalf("log = %q, want substring %q", got, want)
		}
	}
}

func TestWebhookHandlerRejectsBadPayload(t *testing.T) {
	handler := newWebhookHandler(slog.Default())
	req := httptest.NewRequest(http.MethodPost, "/webhook", strings.NewReader("{bad json"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusBadRequest)
	}
}
