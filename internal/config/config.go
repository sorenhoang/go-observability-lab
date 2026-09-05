// Package config holds runtime configuration read from the environment.
//
// ponytail: one struct, env vars only. No config library for a handful of
// values. Each phase adds a field here rather than introducing a framework.
package config

import (
	"os"
	"strconv"
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
}

// Load reads configuration from the environment, applying defaults for any
// value that is unset. It never fails: missing values fall back to defaults,
// and out-of-range values are clamped to something sensible so a typo can't
// put the API into a nonsensical state.
func Load() Config {
	c := Config{
		Addr:            getenv("API_ADDR", ":8080"),
		ShutdownTimeout: getenvDuration("API_SHUTDOWN_TIMEOUT", 10*time.Second),
		SlowMinMs:       getenvInt("API_SLOW_MIN_MS", 50),
		SlowMaxMs:       getenvInt("API_SLOW_MAX_MS", 2000),
		ErrorRate:       getenvFloat("API_ERROR_RATE", 0.3),
	}

	c.ErrorRate = min(max(c.ErrorRate, 0), 1)
	c.SlowMinMs = max(c.SlowMinMs, 0)
	c.SlowMaxMs = max(c.SlowMaxMs, 0)
	c.SlowMinMs = min(c.SlowMinMs, c.SlowMaxMs)

	return c
}

func getenv(key, fallback string) string {
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

func getenvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}

func getenvFloat(key string, fallback float64) float64 {
	if v := os.Getenv(key); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return fallback
}
