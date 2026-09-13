package main

import (
	"testing"

	"github.com/segmentio/kafka-go"
)

// RED PHASE (Task 5). Fails to build until consumeSpanContext exists.

func TestConsumeSpanContextFromHeaders(t *testing.T) {
	tp := "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	sc, ok := consumeSpanContext([]kafka.Header{{Key: "traceparent", Value: []byte(tp)}})
	if !ok || sc.TraceID().String() != "0af7651916cd43dd8448eb211c80319c" {
		t.Fatalf("consumeSpanContext = %+v ok=%v", sc, ok)
	}
}

func TestConsumeSpanContextMissingHeader(t *testing.T) {
	if _, ok := consumeSpanContext(nil); ok {
		t.Fatal("consumeSpanContext(nil) = ok, want false")
	}
}
