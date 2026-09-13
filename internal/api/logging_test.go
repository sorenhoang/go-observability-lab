package api

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sorenhoang/go-observability-lab/internal/obs"
)

func routerWithLogBuffer(t *testing.T) (http.Handler, *bytes.Buffer) {
	t.Helper()
	var buf bytes.Buffer
	logger := slog.New(obs.NewHandler(&buf, slog.LevelInfo))
	return newTestRouterWithLogger(logger), &buf // helper added to router_test.go
}

func TestBusinessRouteEmitsOneCanonicalLine(t *testing.T) {
	router, buf := routerWithLogBuffer(t)
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))

	var reqLines int
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		m := map[string]any{}
		_ = json.Unmarshal([]byte(l), &m)
		if m["msg"] == "request" {
			reqLines++
			if m["route"] != "/users" || m["method"] != "GET" || m["status"] != float64(200) {
				t.Fatalf("canonical line = %v", m)
			}
		}
	}
	if reqLines != 1 {
		t.Fatalf("got %d canonical request lines, want 1", reqLines)
	}
}

func TestMetricsRouteEmitsNoCanonicalLine(t *testing.T) {
	router, buf := routerWithLogBuffer(t)
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if strings.Contains(buf.String(), `"msg":"request"`) {
		t.Fatal("/metrics produced a canonical request line")
	}
}

func TestChaosInjectedFailureLogsWarnWithSameRequestID(t *testing.T) {
	router, buf := routerWithLogBuffer(t)
	router.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodPost, "/admin/chaos", strings.NewReader(`{"enabled":true,"error_ratio":1}`)))
	buf.Reset()
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))

	var warnID, reqID string
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		m := map[string]any{}
		_ = json.Unmarshal([]byte(l), &m)
		switch {
		case m["level"] == "WARN" && strings.Contains(l, "chaos"):
			warnID, _ = m["request_id"].(string)
		case m["msg"] == "request":
			reqID, _ = m["request_id"].(string)
		}
	}
	if warnID == "" || warnID != reqID {
		t.Fatalf("chaos WARN request_id %q != request line request_id %q", warnID, reqID)
	}
}

func TestAdminTokenNeverLogged(t *testing.T) {
	router, buf := routerWithLogBuffer(t)
	router.ServeHTTP(httptest.NewRecorder(),
		httptest.NewRequest(http.MethodGet, "/admin/chaos?token=TOPSECRET", nil))
	if strings.Contains(buf.String(), "TOPSECRET") {
		t.Fatalf("admin token leaked into logs:\n%s", buf.String())
	}
}
