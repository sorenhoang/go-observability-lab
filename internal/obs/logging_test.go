package obs

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

// RED PHASE (Task 1). Fails to build until internal/obs/logging.go provides
// NewHandler(w io.Writer, level slog.Leveler) slog.Handler.

func decodeLastLine(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	trimmed := strings.TrimSpace(buf.String())
	if trimmed == "" {
		t.Fatal("no log output")
	}
	lines := strings.Split(trimmed, "\n")
	var m map[string]any
	last := lines[len(lines)-1]
	if err := json.Unmarshal([]byte(last), &m); err != nil {
		t.Fatalf("log line is not JSON: %v\nline: %s", err, last)
	}
	return m
}

func TestNewHandlerEmitsJSON(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewHandler(&buf, slog.LevelInfo))

	log.Info("hello", "widget", "gear")

	m := decodeLastLine(t, &buf)
	if m["msg"] != "hello" || m["widget"] != "gear" {
		t.Fatalf("decoded = %v, want msg=hello widget=gear", m)
	}
}

func TestNewHandlerOmitsTraceIDWhenAbsent(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewHandler(&buf, slog.LevelInfo))

	log.InfoContext(context.Background(), "no-trace")

	if _, ok := decodeLastLine(t, &buf)["trace_id"]; ok {
		t.Fatal("trace_id present for a context with no trace")
	}
}

func TestNewHandlerInjectsTraceIDFromContext(t *testing.T) {
	const traceID = "0af7651916cd43dd8448eb211c80319c"
	const spanID = "b7ad6b7169203331"

	var buf bytes.Buffer
	log := slog.New(NewHandler(&buf, slog.LevelInfo))
	ctx := contextWithTraceIDs(context.Background(), traceID, spanID)

	log.InfoContext(ctx, "with-trace")

	m := decodeLastLine(t, &buf)
	if m["trace_id"] != traceID {
		t.Fatalf("trace_id = %v, want %q", m["trace_id"], traceID)
	}
	if m["span_id"] != spanID {
		t.Fatalf("span_id = %v, want %q", m["span_id"], spanID)
	}
}

func TestNewHandlerRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(NewHandler(&buf, slog.LevelWarn))

	log.Info("below-threshold")
	if buf.Len() != 0 {
		t.Fatalf("INFO record emitted under a WARN handler: %s", buf.String())
	}

	log.Warn("at-threshold")
	if !strings.Contains(buf.String(), "at-threshold") {
		t.Fatal("WARN record not emitted")
	}
}
