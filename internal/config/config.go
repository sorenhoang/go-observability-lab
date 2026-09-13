// Package config holds runtime configuration read from the environment.
//
// ponytail: one struct, env vars only. No config library for a handful of
// values. Each phase adds a field here rather than introducing a framework.
package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the fully-resolved runtime configuration for the API.
type Config struct {
	// Addr is the listen address for the HTTP server, e.g. ":8080".
	Addr string

	// ShutdownTimeout bounds how long graceful shutdown waits for in-flight
	// requests to finish before the process exits anyway.
	ShutdownTimeout time.Duration

	// SlowMinMs and SlowMaxMs bound simulated latency for GET /slow.
	SlowMinMs int
	SlowMaxMs int

	// ErrorRate controls the default simulated failure probability for GET /error.
	ErrorRate float64

	// AdminToken protects failure-simulation control endpoints when set.
	AdminToken string

	// DatabaseURL points at the lab Postgres database. The API cannot serve
	// without this source of truth.
	DatabaseURL string

	// RedisAddr points at the optional products cache. Redis failures degrade
	// to database reads.
	RedisAddr string

	// KafkaBrokers is the comma-separated Kafka bootstrap list used for
	// best-effort order events.
	KafkaBrokers string

	// LogLevel controls the minimum severity emitted by the structured JSON
	// logger: "debug", "info" (default), "warn", or "error".
	LogLevel string

	// OTLPEndpoint is the OTLP/gRPC collector address for traces, e.g.
	// "tempo:4317". Empty disables tracing entirely (spans still create,
	// never export) — the same optional-dependency pattern as Redis/Kafka.
	OTLPEndpoint string

	// TraceSampleRatio is the fraction of traces sampled, clamped to [0,1].
	TraceSampleRatio float64
}

// SlogLevel maps LogLevel to a slog.Level, defaulting to Info for an unknown
// or empty value rather than failing startup over a typo.
func (c Config) SlogLevel() slog.Level {
	switch strings.ToLower(c.LogLevel) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Load reads configuration from the environment, applying defaults for any
// value that is unset. It never fails: missing values fall back to defaults,
// and out-of-range values are clamped to something sensible so a typo can't
// put the API into a nonsensical state.
func Load() Config {
	c := Config{
		Addr:             Getenv("API_ADDR", ":8080"),
		ShutdownTimeout:  getenvDuration("API_SHUTDOWN_TIMEOUT", 10*time.Second),
		SlowMinMs:        GetenvInt("API_SLOW_MIN_MS", 50),
		SlowMaxMs:        GetenvInt("API_SLOW_MAX_MS", 2000),
		ErrorRate:        GetenvFloat("API_ERROR_RATE", 0.3),
		AdminToken:       Getenv("API_ADMIN_TOKEN", ""),
		DatabaseURL:      Getenv("API_DATABASE_URL", "postgres://lab:lab@postgres:5432/lab?sslmode=disable"),
		RedisAddr:        Getenv("API_REDIS_ADDR", "redis:6379"),
		KafkaBrokers:     Getenv("API_KAFKA_BROKERS", "kafka:9092"),
		LogLevel:         Getenv("API_LOG_LEVEL", "info"),
		OTLPEndpoint:     Getenv("API_OTLP_ENDPOINT", ""),
		TraceSampleRatio: GetenvFloat("API_TRACE_SAMPLE_RATIO", 1.0),
	}

	c.ErrorRate = min(max(c.ErrorRate, 0), 1)
	c.SlowMinMs = max(c.SlowMinMs, 0)
	c.SlowMaxMs = max(c.SlowMaxMs, 0)
	c.SlowMinMs = min(c.SlowMinMs, c.SlowMaxMs)
	c.TraceSampleRatio = min(max(c.TraceSampleRatio, 0), 1)

	return c
}

// Getenv, GetenvInt, and GetenvFloat are exported so cmd/consumer — which
// has its own small set of env vars and no Config struct of its own — reads
// the environment the same way the API does, instead of a second copy of
// the same three functions.

func Getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getenvDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return fallback
}

func GetenvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func GetenvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}
