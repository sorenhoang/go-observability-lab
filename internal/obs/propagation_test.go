package obs

import (
	"testing"

	"go.opentelemetry.io/otel/trace"
)

// RED PHASE (Task 4). Fails to build until internal/obs/propagation.go
// provides FormatTraceparent / ParseTraceparent.

func TestTraceparentRoundTrip(t *testing.T) {
	tid, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
	sid, _ := trace.SpanIDFromHex("b7ad6b7169203331")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: tid, SpanID: sid, TraceFlags: trace.FlagsSampled, Remote: true})

	got, ok := ParseTraceparent(FormatTraceparent(sc))
	if !ok {
		t.Fatal("round-trip parse returned ok=false")
	}
	if got.TraceID() != tid || got.SpanID() != sid || !got.IsSampled() {
		t.Fatalf("round-trip lost data: %+v", got)
	}
}

func TestParseTraceparentRejectsMalformed(t *testing.T) {
	bad := []string{
		"",
		"00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331",    // missing flags
		"01-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01", // bad version
		"00-zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz-b7ad6b7169203331-01", // non-hex
		"00-00000000000000000000000000000000-b7ad6b7169203331-01", // zero trace id
		"00-0af7651916cd43dd8448eb211c80319c-0000000000000000-01", // zero span id
		"00-0af7-b7ad6b7169203331-01",                             // short trace id
	}
	for _, v := range bad {
		if _, ok := ParseTraceparent(v); ok {
			t.Fatalf("ParseTraceparent(%q) = ok, want rejected", v)
		}
	}
}
