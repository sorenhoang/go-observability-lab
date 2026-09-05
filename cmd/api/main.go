// Command api is the entrypoint for the observability-lab HTTP service.
//
// Phase 0: starts an http.Server with sane timeouts and shuts down
// gracefully on SIGINT/SIGTERM so that in-flight requests are drained.
// A clean shutdown also matters for the lab itself: later phases test the
// Prometheus `up` metric by stopping this process.
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

	"github.com/sorenhoang/go-observability-lab/internal/api"
	"github.com/sorenhoang/go-observability-lab/internal/config"
	"github.com/sorenhoang/go-observability-lab/internal/metrics"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	cfg := config.Load()
	m := metrics.New()

	srv := &http.Server{
		Addr:    cfg.Addr,
		Handler: api.NewRouter(cfg, m),
		// ReadHeaderTimeout guards against slowloris; without it gosec (G112)
		// flags the server and a single slow client can pin a connection.
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Signal-aware context: cancelled on the first SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", cfg.Addr)
		// ListenAndServe always returns a non-nil error; ErrServerClosed is
		// the expected one after Shutdown and must not be treated as failure.
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		slog.Error("server failed", "err", err)
		os.Exit(1)
	case <-ctx.Done():
		slog.Info("shutting down", "timeout", cfg.ShutdownTimeout)
	}

	// Bounded drain: stop accepting new connections, wait for in-flight
	// requests up to the timeout, then exit regardless.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed, forcing close", "err", err)
		_ = srv.Close()
		os.Exit(1)
	}
	slog.Info("stopped")
}
