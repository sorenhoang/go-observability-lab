package obs

import (
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// FormatTraceparent and ParseTraceparent hand-roll the W3C trace-context
// header for Kafka message headers, where there is no HTTP request to run
// otel's propagation.TraceContext carrier against — Task 5 uses these to
// thread a trace across the producer/consumer boundary.

// FormatTraceparent renders sc as a W3C level-1 traceparent value:
// "00-<32 hex trace id>-<16 hex span id>-<2 hex flags>".
func FormatTraceparent(sc trace.SpanContext) string {
	flags := byte(0)
	if sc.IsSampled() {
		flags = 1
	}
	return fmt.Sprintf("00-%s-%s-%02x", sc.TraceID(), sc.SpanID(), flags)
}

// ParseTraceparent parses a W3C level-1 traceparent value. It returns
// ok=false for anything that isn't exactly that format: wrong field count,
// a version other than "00", non-hex IDs, wrong-length IDs, or an all-zero
// trace/span ID (W3C reserves all-zero as invalid).
func ParseTraceparent(v string) (trace.SpanContext, bool) {
	parts := strings.Split(v, "-")
	if len(parts) != 4 {
		return trace.SpanContext{}, false
	}
	version, traceIDHex, spanIDHex, flagsHex := parts[0], parts[1], parts[2], parts[3]

	if version != "00" {
		return trace.SpanContext{}, false
	}
	if len(traceIDHex) != 32 || len(spanIDHex) != 16 || len(flagsHex) != 2 {
		return trace.SpanContext{}, false
	}

	traceID, err := trace.TraceIDFromHex(traceIDHex)
	if err != nil || !traceID.IsValid() {
		return trace.SpanContext{}, false
	}
	spanID, err := trace.SpanIDFromHex(spanIDHex)
	if err != nil || !spanID.IsValid() {
		return trace.SpanContext{}, false
	}

	var flags trace.TraceFlags
	if _, err := fmt.Sscanf(flagsHex, "%02x", &flags); err != nil {
		return trace.SpanContext{}, false
	}

	return trace.NewSpanContext(trace.SpanContextConfig{
		TraceID:    traceID,
		SpanID:     spanID,
		TraceFlags: flags,
		Remote:     true,
	}), true
}
