package obs

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
)

// tracerName is the instrumentation-library name every span in this app is
// recorded under — the lab is one Go module, so one name is enough.
const tracerName = "go-observability-lab"

// InitTracer wires the OTel SDK as a plain span factory + exporter: the app
// hand-writes every span (TraceHTTP, Task 5's child spans), never reaching
// for go.opentelemetry.io/otel/contrib/instrumentation. An empty endpoint
// means "no Tempo configured" — spans are still created (so TraceHTTP and
// the logger's trace_id injection keep working) but never exported, and
// shutdown is a no-op. This never errors on an empty endpoint: a lab running
// without Tempo must still start.
func InitTracer(ctx context.Context, service, endpoint string, ratio float64) (func(context.Context) error, error) {
	res, err := resource.New(ctx, resource.WithAttributes(semconv.ServiceName(service)))
	if err != nil {
		return nil, err
	}

	if endpoint == "" {
		tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res))
		otel.SetTracerProvider(tp)
		otel.SetTextMapPropagator(propagation.TraceContext{})
		return func(context.Context) error { return nil }, nil
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	return tp.Shutdown, nil
}

// Tracer returns the app's single tracer off whatever TracerProvider is
// currently registered globally (set by InitTracer, or overridden by a test
// via otel.SetTracerProvider).
func Tracer() trace.Tracer {
	return otel.Tracer(tracerName)
}
