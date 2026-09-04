// Package config holds runtime configuration read from the environment.
//
// ponytail: one struct, env vars only. No config library for a handful of
// values. Each phase adds a field here rather than introducing a framework.
package config

import (
	"os"
	"time"
)

// Config is the fully-resolved runtime configuration for the API.
type Config struct {
	// Addr is the listen address for the HTTP server, e.g. ":8080".
	Addr string

	// ShutdownTimeout bounds how long graceful shutdown waits for in-flight
	// requests to finish before the process exits anyway.
	ShutdownTimeout time.Duration
}

// Load reads configuration from the environment, applying defaults for any
// value that is unset. It never fails: missing values fall back to defaults.
func Load() Config {
	return Config{
		Addr:            getenv("API_ADDR", ":8080"),
		ShutdownTimeout: getenvDuration("API_SHUTDOWN_TIMEOUT", 10*time.Second),
	}
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
