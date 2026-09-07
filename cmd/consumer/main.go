package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/segmentio/kafka-go"
	"github.com/sorenhoang/go-observability-lab/internal/events"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	brokers := splitBrokers(getenv("CONSUMER_KAFKA_BROKERS", "kafka:9092"))
	delay := time.Duration(getenvInt("CONSUMER_DELAY_MS", 50)) * time.Millisecond

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
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
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
			start := time.Now()
			time.Sleep(delay)
			processing.Observe(time.Since(start).Seconds())
			consumed.Inc()
			slog.Info("processed order event", "topic", msg.Topic, "partition", msg.Partition, "offset", msg.Offset)
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

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func splitBrokers(brokers string) []string {
	var out []string
	for _, broker := range strings.Split(brokers, ",") {
		broker = strings.TrimSpace(broker)
		if broker != "" {
			out = append(out, broker)
		}
	}
	return out
}
