package obs

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// RED PHASE (Task 1). Fails to build until internal/obs/middleware.go provides:
//
//	PanicGuard(base *slog.Logger) func(http.Handler) http.Handler
//	RequestLogger(base *slog.Logger, routeFunc func(*http.Request) string) func(http.Handler) http.Handler
//
// TraceHTTP is added in Task 4/5 (Phase 11) and tested there.

func newTestLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(NewHandler(&buf, slog.LevelInfo)), &buf
}

func lastLogLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	m := map[string]any{}
	last := lines[len(lines)-1]
	if err := json.Unmarshal([]byte(last), &m); err != nil {
		t.Fatalf("log line is not JSON: %v (%q)", err, last)
	}
	return m
}

func fixedRoute(name string) func(*http.Request) string {
	return func(*http.Request) string { return name }
}

func TestRequestLoggerEmitsExactlyOneCanonicalLine(t *testing.T) {
	log, buf := newTestLogger()
	h := RequestLogger(log, fixedRoute("/users"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "ok")
	}))

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/users", nil))

	if extra := strings.Count(strings.TrimSpace(buf.String()), "\n"); extra != 0 {
		t.Fatalf("emitted %d extra log lines, want exactly one canonical line:\n%s", extra, buf.String())
	}
	m := lastLogLine(t, buf)
	if m["msg"] != "request" || m["method"] != "GET" || m["route"] != "/users" {
		t.Fatalf("canonical line = %v", m)
	}
	if m["status"] != float64(200) {
		t.Fatalf("status = %v, want 200", m["status"])
	}
	if _, ok := m["duration_ms"]; !ok {
		t.Fatal("no duration_ms field")
	}
	if _, ok := m["request_id"]; !ok {
		t.Fatal("no request_id field")
	}
}

func TestRequestLoggerSeverityKeysOffStatus(t *testing.T) {
	for _, c := range []struct {
		status int
		level  string
	}{
		{http.StatusOK, "INFO"},
		{http.StatusNotFound, "WARN"},
		{http.StatusInternalServerError, "ERROR"},
	} {
		log, buf := newTestLogger()
		h := RequestLogger(log, fixedRoute("/x"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(c.status)
		}))
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

		if got := lastLogLine(t, buf)["level"]; got != c.level {
			t.Fatalf("status %d logged at level %v, want %s", c.status, got, c.level)
		}
	}
}

func TestRequestLoggerNeverLogsBodyOrToken(t *testing.T) {
	log, buf := newTestLogger()
	h := RequestLogger(log, fixedRoute("/admin/chaos"))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))

	body := `{"enabled":true,"leak_me":"HUNTER2"}`
	req := httptest.NewRequest(http.MethodPost, "/admin/chaos?token=SUPERSECRET", strings.NewReader(body))
	h.ServeHTTP(httptest.NewRecorder(), req)

	if out := buf.String(); strings.Contains(out, "HUNTER2") || strings.Contains(out, "SUPERSECRET") {
		t.Fatalf("sensitive request data leaked into logs:\n%s", out)
	}
}

func TestPanicGuardLogsPanicAndReturns500(t *testing.T) {
	log, buf := newTestLogger()
	h := PanicGuard(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	}))

	rec := httptest.NewRecorder()
	// Must not panic out of ServeHTTP.
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	m := lastLogLine(t, buf)
	if m["level"] != "ERROR" {
		t.Fatalf("panic logged at %v, want ERROR", m["level"])
	}
	if _, ok := m["panic"]; !ok {
		t.Fatal("no panic field in the log line")
	}
}

func TestRequestLoggerRecordsPanicAsError(t *testing.T) {
	log, buf := newTestLogger()
	// PanicGuard sits inside RequestLogger, exactly as the router wires it.
	h := RequestLogger(log, fixedRoute("/x"))(
		PanicGuard(log)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			panic("boom")
		})),
	)

	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/x", nil))

	m := lastLogLine(t, buf)
	if m["msg"] != "request" || m["status"] != float64(500) || m["level"] != "ERROR" {
		t.Fatalf("canonical line after a panic = %v, want msg=request status=500 level=ERROR", m)
	}
}
