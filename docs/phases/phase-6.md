# Phase 6 — Failure simulation

**Status: DONE.** Runtime chaos control plane (`/admin/chaos`, `/cpu`, `/leak`) behind an `API_ADMIN_TOKEN` guard, injection middleware wired `instrument(chaos(h))`, a `chaos_enabled` gauge + annotation, `loadgen/spike.js`, a Runtime/Saturation dashboard row, and the failure playbook. Every fault verified live against Prometheus.

## Objective

Break the service on purpose, from a control plane you can toggle at runtime,
and watch each fault land on the dashboard. By the end you should be able to
*predict* which panel moves before you look.

## Concepts to internalize

- **A latency problem, an error problem, and a saturation problem look
  different on a dashboard.** Latency → the Duration panel and heatmap climb,
  Rate is flat, Errors flat. Errors → Error % and 5xx-by-route climb, Duration
  maybe flat. Saturation → in-flight climbs, then latency follows, then errors
  follow as things time out. Learning to read which one you're looking at is
  the whole point of the phase.
- **Runtime metrics matter now.** `go_goroutines`,
  `go_memstats_heap_inuse_bytes`, `process_cpu_seconds_total` came free with
  the Go/process collectors in Phase 2 and have been boring flat lines. A CPU
  burn or a memory leak makes them the most important panel on the screen.
- **Control plane vs data plane.** Your chaos toggles (`/admin/chaos`, `/cpu`,
  `/leak`) must not themselves be subject to the chaos, and shouldn't be
  reachable by accident — they go behind a token and are excluded from the
  fault-injection middleware.

## What to build

### 1. `internal/api/chaos.go` — runtime-toggleable state + injection

**State**, swapped atomically so reads never lock:

```go
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
	state atomic.Pointer[chaosState]
}

func NewChaos() *Chaos {
	c := &Chaos{}
	c.state.Store(&chaosState{}) // disabled zero value
	return c
}

func (c *Chaos) get() chaosState  { return *c.state.Load() }
func (c *Chaos) set(s chaosState) { c.state.Store(&s) }
```

**Injection middleware** — wraps the *business* routes only:

```go
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
```

**Middleware order matters:** it must be `instrument(chaos(handler))`, not the
other way round. The RED metrics have to *see* the injected latency and 500s —
that's the entire point. If `chaos` wrapped `instrument`, the dashboard
wouldn't move and you'd have learned nothing.

### 2. Chaos control handlers

- `GET /admin/chaos` → return the current `chaosState` as JSON.
- `POST /admin/chaos` → decode a `chaosState`, **clamp** (`LatencyMs` to
  `[0, chaosMaxLatencyMs]`, `ErrorRatio` to `[0,1]`), `c.set(...)`, return the
  stored state. `DisallowUnknownFields`, same as `/orders`.

### 3. `/cpu` and `/leak` — direct resource pressure

```go
// GET /cpu?seconds=N  — busy-loop on every core for N seconds (clamped).
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
```

```go
// leaked holds allocations that are deliberately never freed until /leak/reset.
// ponytail: a package-level slice IS the leak — that's the point, not a bug.
var (
	leakMu sync.Mutex
	leaked [][]byte
)

// GET  /leak?mb=N   — allocate N MB and hold it.
// POST /leak/reset  — drop it all and force a GC.
```

`/leak` must touch the memory it allocates (write a byte per page) or Go's
allocator may not actually commit the pages and `heap_inuse_bytes` won't move.

### 4. Admin token guard

Add `AdminToken string` to config (`API_ADMIN_TOKEN`). A `requireAdmin`
middleware:

- token unset → allow, but log a one-time warning at startup that the chaos
  endpoints are open.
- token set → require `Authorization: Bearer <token>` (or `?token=`), else 401.

Wrap `/admin/chaos`, `/cpu`, and `/leak` (and `/leak/reset`) with it. These are
the endpoints that can wedge the lab.

### 5. Router wiring

```go
chaos := NewChaos()
h.chaos = chaos
instrument := m.Instrument(routePattern)

business := func(fn http.HandlerFunc) http.Handler {
	return instrument(chaos.Middleware(fn))
}
plain := func(fn http.HandlerFunc) http.Handler { // instrumented, never chaos-affected
	return instrument(http.HandlerFunc(fn))
}
control := func(fn http.HandlerFunc) http.Handler {
	return instrument(requireAdmin(cfg, http.HandlerFunc(fn)))
}

mux.Handle("GET /health",    plain(handleHealth))    // a health probe must stay honest
mux.Handle("GET /users",     business(handleUsers))
// ... products, orders, slow, error via business()
mux.Handle("GET /cpu",           control(h.handleCPU))
mux.Handle("GET /leak",          control(h.handleLeak))
mux.Handle("POST /leak/reset",   control(h.handleLeakReset))
mux.Handle("GET /admin/chaos",   control(h.handleGetChaos))
mux.Handle("POST /admin/chaos",  control(h.handleSetChaos))
mux.Handle("GET /metrics",    promhttp.HandlerFor(m.Registry(), promhttp.HandlerOpts{}))
```

### 6. Optional but recommended: a `chaos_enabled` gauge

```go
chaosEnabled prometheus.Gauge   // in Metrics, name "chaos_enabled", 1 or 0
```

Set it in `c.set()`. Then a Grafana **annotation** query on `changes(chaos_enabled[$__rate_interval]) > 0`
marks the exact moment chaos flipped on every panel — makes the playbook
screenshots legible.

### 7. `loadgen/spike.js` — traffic spike scenario

```javascript
import http from 'k6/http';
const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export const options = {
  scenarios: {
    spike: {
      executor: 'ramping-arrival-rate',
      startRate: 20, timeUnit: '1s',
      preAllocatedVUs: 50, maxVUs: 300,
      stages: [
        { target: 20,  duration: '1m' },   // baseline
        { target: 250, duration: '30s' },  // ~12x ramp
        { target: 250, duration: '1m' },   // hold
        { target: 20,  duration: '30s' },  // recover
        { target: 20,  duration: '1m' },
      ],
    },
  },
};

export default function () {
  http.get(`${BASE_URL}/users`);
}
```

Add to compose as a second command / or a `make spike` target:

```makefile
spike: ## Run the k6 traffic-spike scenario
	docker compose --profile load run --rm --no-deps -e K6_SCRIPT=/scripts/spike.js k6 run /scripts/spike.js
```

### 8. Dashboard — "Runtime / Saturation" row

Add a row to `red.json` (build in UI, re-export). Panels:

| Panel | Query | Unit |
|-------|-------|------|
| Goroutines | `go_goroutines` | short |
| Heap in use | `go_memstats_heap_inuse_bytes` | bytes |
| GC time /s | `rate(go_gc_duration_seconds_sum[$__rate_interval])` | s |
| CPU cores used | `rate(process_cpu_seconds_total[$__rate_interval])` | short |

If you added the `chaos_enabled` gauge: a stat panel for it, and an annotation.

### 9. `docs/06-failure-playbook.md`

A table: for each fault, the exact command to trigger it, the panel(s) that
move, and *what the movement means*. Leave room for a screenshot per fault.

| Fault | Trigger | Panels that move | Reading |
|-------|---------|------------------|---------|
| Injected latency | `POST /admin/chaos {enabled:true, latency_ms:500}` | Duration ↑, heatmap band shifts up, in-flight ↑; Rate/Errors flat | "everything got slow, nothing's failing" — a downstream dependency or lock, not a crash |
| Injected errors | `POST /admin/chaos {enabled:true, error_ratio:0.3}` | Error % ↑, 5xx-by-route ↑ across *all* routes; Duration ~flat | service-wide failure, not one bad endpoint |
| CPU burn | `GET /cpu?seconds=20` | CPU cores ↑ to GOMAXPROCS, then Duration ↑ (scheduler starved), goroutines maybe ↑ | resource exhaustion — latency is a *symptom*, CPU is the cause |
| Memory leak | `GET /leak?mb=200` (repeat) | Heap in use ↑ and stays, GC time /s ↑ | leak — the never-coming-back-down slope is the signature |
| Traffic spike | `make spike` | Rate ↑ 12x, in-flight ↑, Duration ↑ under contention, then recovers | load, not a bug — the recover stage should bring everything back |

---

## Definition of Done

```sh
make up && make load
# latency
curl -s -XPOST localhost:8080/admin/chaos -d '{"enabled":true,"latency_ms":400}'
# ... watch Duration panel climb, Rate stay flat ...
curl -s -XPOST localhost:8080/admin/chaos -d '{"enabled":false}'
# cpu
curl -s "localhost:8080/cpu?seconds=15"
# leak
curl -s "localhost:8080/leak?mb=150"; curl -s "localhost:8080/leak?mb=150"
curl -s -XPOST localhost:8080/leak/reset
# spike
make spike
```

- [ ] `POST /admin/chaos` toggles latency + error injection at runtime; `GET` reflects it
- [ ] Injected latency raises the Duration panel + P95 within one scrape, Rate stays flat
- [ ] Injected errors raise Error % across all business routes, not just `/error`
- [ ] `/cpu?seconds=N` moves `rate(process_cpu_seconds_total[...])`; N is clamped
- [ ] `/leak?mb=N` grows `go_memstats_heap_inuse_bytes`; `/leak/reset` returns it (after GC)
- [ ] `make spike` produces a visible ramp-and-recover on Rate + in-flight
- [ ] Chaos endpoints require the token when `API_ADMIN_TOKEN` is set; open + warn when not
- [ ] `/admin`, `/cpu`, `/leak` are **not** affected by the injection middleware
- [ ] "Runtime / Saturation" dashboard row added, all 4 panels populate
- [ ] `chaos_test.go` covers: disabled passthrough, error_ratio=1 → 500, latency adds delay, clamping, `/leak` grow+reset
- [ ] `docs/06-failure-playbook.md` written with the fault→signal table
- [ ] Committed, tagged `phase-6`

## Traps to notice

- Middleware order: `instrument(chaos(h))`. Get it backwards and the dashboard
  shows nothing.
- A `/leak` that doesn't write to the memory it allocates won't move
  `heap_inuse_bytes` — Go won't commit untouched pages.
- `/cpu` without a `r.Context().Done()` check keeps burning after the client
  gives up — same lesson as `/slow` in Phase 1.
- Forgetting to clamp `seconds` / `mb` — someone (you) will type `/cpu?seconds=99999`.
- Applying chaos to `/health` will flap your Docker/Compose healthcheck if you
  ever add one — this is why control-plane routes stay out of the chaos
  middleware.
