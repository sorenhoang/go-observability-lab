package config

import (
	"testing"
)

func TestLoadClampsOutOfRangeValues(t *testing.T) {
	t.Setenv("API_ERROR_RATE", "5")
	t.Setenv("API_SLOW_MIN_MS", "-10")
	t.Setenv("API_SLOW_MAX_MS", "100")

	c := Load()

	if c.ErrorRate != 1 {
		t.Fatalf("ErrorRate = %v, want clamped to 1", c.ErrorRate)
	}
	if c.SlowMinMs != 0 {
		t.Fatalf("SlowMinMs = %d, want clamped to 0", c.SlowMinMs)
	}
	if c.SlowMaxMs != 100 {
		t.Fatalf("SlowMaxMs = %d, want 100", c.SlowMaxMs)
	}
}

func TestLoadClampsMinAboveMax(t *testing.T) {
	t.Setenv("API_SLOW_MIN_MS", "500")
	t.Setenv("API_SLOW_MAX_MS", "100")

	c := Load()

	if c.SlowMinMs != 100 {
		t.Fatalf("SlowMinMs = %d, want clamped down to SlowMaxMs (100)", c.SlowMinMs)
	}
}

func TestLoadDefaults(t *testing.T) {
	// Force-unset so the test is hermetic regardless of the caller's env.
	for _, k := range []string{"API_ADDR", "API_ERROR_RATE", "API_SLOW_MIN_MS", "API_SLOW_MAX_MS"} {
		t.Setenv(k, "")
	}

	c := Load()

	if c.Addr != ":8080" {
		t.Fatalf("Addr = %q, want :8080", c.Addr)
	}
	if c.ErrorRate != 0.3 {
		t.Fatalf("ErrorRate = %v, want 0.3", c.ErrorRate)
	}
	if c.SlowMinMs != 50 || c.SlowMaxMs != 2000 {
		t.Fatalf("slow bounds = [%d,%d], want [50,2000]", c.SlowMinMs, c.SlowMaxMs)
	}
}
