// Command consumer reads order events off Kafka (topic "orders", group
// "order-processors"), does token processing work, and exposes its own
// /metrics + traces on :9002. Independent binary from cmd/api so the two
// scale and fail separately.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/segmentio/kafka-go"
	"github.com/sorenhoang/go-observability-lab/internal/config"
	"github.com/sorenhoang/go-observability-lab/internal/events"
	"github.com/sorenhoang/go-observability-lab/internal/obs"
	"go.opentelemetry.io/otel/trace"
)

func main() {
	logger := slog.New(obs.NewHandler(os.Stdout, slog.LevelInfo))
	slog.SetDefault(logger)

	brokers := events.SplitBrokers(config.Getenv("CONSUMER_KAFKA_BROKERS", "kafka:9092"))
	delay := time.Duration(config.GetenvInt("CONSUMER_DELAY_MS", 50)) * time.Millisecond

	startupCtx, startupCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer startupCancel()
	shutdownTracer, err := obs.InitTracer(startupCtx, "consumer",
		config.Getenv("CONSUMER_OTLP_ENDPOINT", ""),
		config.GetenvFloat("CONSUMER_TRACE_SAMPLE_RATIO", 1.0),
	)
	if err != nil {
		slog.Error("tracer init failed", "err", err)
		os.Exit(1)
	}
	defer func() {
		if err := shutdownTracer(context.Background()); err != nil {
			slog.Warn("tracer shutdown failed", "err", err)
		}
	}()

	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	consumed := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "orders_consumed_total",
		Help: "Total order events consumed.",
	})
	processing := prometheus.NewHistogram(prometheus.HistogramOpts{
		Name:    "order_processing_duration_seconds",
		Help:    "Duration spent processing order events.",
		Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2, 5},
	})
	reg.MustRegister(consumed, processing)

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{EnableOpenMetrics: true}))
	srv := &http.Server{
		Addr:              ":9002",
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("consumer metrics listening", "addr", srv.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers:     brokers,
		Topic:       events.OrdersTopic,
		GroupID:     "order-processors",
		StartOffset: kafka.FirstOffset,
	})
	defer reader.Close()

	consumerErr := make(chan error, 1)
	go func() {
		for {
			msg, err := reader.ReadMessage(ctx)
			if err != nil {
				if ctx.Err() != nil {
					return
				}
				consumerErr <- err
				return
			}
			msgCtx := context.Background()
			if sc, ok := consumeSpanContext(msg.Headers); ok {
				msgCtx = trace.ContextWithRemoteSpanContext(msgCtx, sc)
			}
			_, span := obs.Tracer().Start(msgCtx, "consume orders", trace.WithSpanKind(trace.SpanKindConsumer))

			start := time.Now()
			time.Sleep(delay)
			processing.Observe(time.Since(start).Seconds())
			consumed.Inc()
			slog.Info("processed order event", "topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset)
			span.End()
		}
	}()

	select {
	case err := <-serverErr:
		slog.Error("metrics server failed", "err", err)
		os.Exit(1)
	case err := <-consumerErr:
		slog.Error("consumer failed", "err", err)
		os.Exit(1)
	case <-ctx.Done():
		slog.Info("consumer shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("metrics shutdown failed", "err", err)
		_ = srv.Close()
		os.Exit(1)
	}
}

// consumeSpanContext extracts the W3C trace context the producer attached to
// the Kafka message. There's no HTTP request here for the OTel propagator to
// read a header off, so this hand-rolls the same parse obs.TraceHTTP gets for
// free from the propagator on the HTTP side.
func consumeSpanContext(headers []kafka.Header) (trace.SpanContext, bool) {
	for _, h := range headers {
		if h.Key == "traceparent" {
			return obs.ParseTraceparent(string(h.Value))
		}
	}
	return trace.SpanContext{}, false
}
