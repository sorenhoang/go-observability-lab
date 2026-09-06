package api

import (
	"crypto/subtle"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/sorenhoang/go-observability-lab/internal/config"
	"github.com/sorenhoang/go-observability-lab/internal/metrics"
)

const (
	chaosMaxLatencyMs = 10_000
	cpuMaxSeconds     = 30
	leakMaxMB         = 512
)

type chaosState struct {
	Enabled    bool    `json:"enabled"`
	LatencyMs  int     `json:"latency_ms"`
	ErrorRatio float64 `json:"error_ratio"`
}

type Chaos struct {
	state   atomic.Pointer[chaosState]
	metrics *metrics.Metrics
}

func NewChaos(m *metrics.Metrics) *Chaos {
	c := &Chaos{metrics: m}
	c.state.Store(&chaosState{})
	return c
}

func (c *Chaos) get() chaosState {
	return *c.state.Load()
}

func (c *Chaos) set(s chaosState) {
	c.state.Store(&s)
	if c.metrics != nil {
		c.metrics.ChaosEnabled(s.Enabled)
	}
}

func (c *Chaos) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s := c.get()
		if s.Enabled {
			if s.LatencyMs > 0 {
				select {
				case <-time.After(time.Duration(s.LatencyMs) * time.Millisecond):
				case <-r.Context().Done():
					return
				}
			}
			if s.ErrorRatio > 0 && rand.Float64() < s.ErrorRatio {
				writeError(w, http.StatusInternalServerError, "chaos: injected failure")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Handlers) handleGetChaos(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, h.chaos.get())
}

func (h *Handlers) handleSetChaos(w http.ResponseWriter, r *http.Request) {
	var req chaosState
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	req.LatencyMs = clamp(req.LatencyMs, 0, chaosMaxLatencyMs)
	req.ErrorRatio = min(max(req.ErrorRatio, 0), 1)
	h.chaos.set(req)
	writeJSON(w, http.StatusOK, req)
}

func requireAdmin(cfg config.Config, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if cfg.AdminToken == "" {
			next.ServeHTTP(w, r)
			return
		}

		presented := r.URL.Query().Get("token")
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Bearer ") {
			presented = strings.TrimPrefix(auth, "Bearer ")
		}
		// Constant-time compare: a plain == leaks the token length and prefix
		// through response timing. Cheap habit worth keeping.
		if subtle.ConstantTimeCompare([]byte(presented), []byte(cfg.AdminToken)) == 1 {
			next.ServeHTTP(w, r)
			return
		}

		writeError(w, http.StatusUnauthorized, "admin token required")
	})
}

func (h *Handlers) handleCPU(w http.ResponseWriter, r *http.Request) {
	seconds := clampAtoi(r.URL.Query().Get("seconds"), 5, 0, cpuMaxSeconds)
	deadline := time.Now().Add(time.Duration(seconds) * time.Second)

	var wg sync.WaitGroup
	for range runtime.GOMAXPROCS(0) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for time.Now().Before(deadline) {
				select {
				case <-r.Context().Done():
					return
				default:
				}
			}
		}()
	}
	wg.Wait()
	writeJSON(w, http.StatusOK, map[string]int{"burned_seconds": seconds})
}

var (
	leakMu sync.Mutex
	leaked [][]byte
)

func (h *Handlers) handleLeak(w http.ResponseWriter, r *http.Request) {
	mb := clampAtoi(r.URL.Query().Get("mb"), 10, 0, leakMaxMB)
	buf := make([]byte, mb*1024*1024)
	for i := 0; i < len(buf); i += 4096 {
		buf[i] = 1
	}

	leakMu.Lock()
	leaked = append(leaked, buf)
	total := leakedMBLocked()
	leakMu.Unlock()

	writeJSON(w, http.StatusOK, map[string]int{
		"allocated_mb": mb,
		"leaked_mb":    total,
	})
}

func (h *Handlers) handleLeakReset(w http.ResponseWriter, _ *http.Request) {
	leakMu.Lock()
	leaked = nil
	leakMu.Unlock()

	runtime.GC()
	writeJSON(w, http.StatusOK, map[string]int{"leaked_mb": 0})
}

func leakedMBLocked() int {
	total := 0
	for _, buf := range leaked {
		total += len(buf)
	}
	return total / 1024 / 1024
}

func clampAtoi(v string, fallback, minValue, maxValue int) int {
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return clamp(n, minValue, maxValue)
}

func clamp(n, minValue, maxValue int) int {
	if n < minValue {
		return minValue
	}
	if n > maxValue {
		return maxValue
	}
	return n
}
