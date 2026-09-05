# Phase 2 — Prometheus instrumentation

**Status: TODO — you code this manually, then ask for a review.**

**This is the core learning phase. Go slow.** Everything before it was setup;
everything after it reuses what you build here.

## Objective

Expose four RED metrics by hand, plus a Summary kept only as a contrast. No
Prometheus *server* yet (Phase 3) — you're building the thing that gets
scraped, and verifying it by reading `:8080/metrics` yourself.

## Concepts to internalize

- **Counter** — only goes up (or resets to 0 on restart). You never chart its
  raw value; you chart `rate()`. Ends in `_total`.
- **Gauge** — goes up and down. Chart it directly.
- **Histogram** — buckets observations (`_bucket`) + `_sum` + `_count`. Lets you
  compute *any* quantile later, in PromQL, aggregated across instances.
- **Summary** — quantiles computed client-side at observation time, baked into
  the exposition. Cannot be meaningfully aggregated across instances (you can't
  average two P95s and get a real P95). You'll add one, see the limitation,
  then stop using it.
- **Label / cardinality** — each unique label-value combination is its own time
  series. Bound your label sets or Prometheus's memory grows without limit.
- **The registry** — a `*prometheus.Registry` is the set of metrics an
  exposition handler serves. This lab uses a **dedicated registry**, not the
  global default one client_golang gives you for free — an explicit registry
  is a constructor argument, testable, no hidden global state.
- **Exposition format** — the plain text at `/metrics`. Read it directly with
  curl; it's the actual contract Prometheus scrapes.

## Add the dependency

```sh
go get github.com/prometheus/client_golang
go mod tidy
```

---

## Step 1 — Metric definitions + registry: `internal/metrics/metrics.go`

```go
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Metrics owns every metric this service exposes and the registry they're
// served from. One instance, constructed once in main, passed into the
// router — never package-level globals, so tests can construct a fresh one.
type Metrics struct {
	registry *prometheus.Registry

	requestsTotal   *prometheus.CounterVec
	requestDuration *prometheus.HistogramVec
	// requestDurationSummary exists purely as a teaching contrast to the
	// Histogram above — see docs/02-metrics.md for why it's abandoned after
	// this phase. Never add a second Summary "for real" in this lab.
	requestDurationSummary *prometheus.SummaryVec
	requestsInProgress     *prometheus.GaugeVec
	ordersCreated          prometheus.Counter
}

func New() *Metrics {
	reg := prometheus.NewRegistry()
	// Go runtime + process stats (goroutines, GC, heap, CPU) — free, and
	// they matter starting Phase 6.
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	m := &Metrics{
		registry: reg,
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests, by method, route, and status.",
		}, []string{"method", "route", "status"}),

		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name: "http_request_duration_seconds",
			Help: "HTTP request duration in seconds, by method and route.",
			// Bracket the range you actually see: /error ~instant, /slow up
			// to 2s by default. Buckets that don't straddle your real
			// latencies give a useless histogram_quantile in Phase 4.
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2, 3},
		}, []string{"method", "route"}),

		requestDurationSummary: prometheus.NewSummaryVec(prometheus.SummaryOpts{
			Name: "http_request_duration_summary_seconds",
			Help: "Teaching contrast only — same observations as the " +
				"histogram above, as a client-side Summary. Cannot be " +
				"aggregated across instances; see docs/02-metrics.md.",
			Objectives: map[float64]float64{0.5: 0.05, 0.95: 0.01, 0.99: 0.001},
		}, []string{"method", "route"}),

		requestsInProgress: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "http_requests_in_progress",
			Help: "In-flight HTTP requests, by route.",
		}, []string{"route"}),

		ordersCreated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "orders_created_total",
			Help: "Total orders successfully created.",
		}),
	}

	reg.MustRegister(
		m.requestsTotal,
		m.requestDuration,
		m.requestDurationSummary,
		m.requestsInProgress,
		m.ordersCreated,
	)
	return m
}

// Registry exposes the registry so the router can mount /metrics against it.
func (m *Metrics) Registry() *prometheus.Registry { return m.registry }

// OrderCreated records a successfully created order. Called from the orders
// handler, not from the RED middleware — it's a business metric, not HTTP.
func (m *Metrics) OrderCreated() { m.ordersCreated.Inc() }
```

**Why explicit `Help` strings, buckets, and label names, all typed out:** this
*is* the exposition format. Every field here is a line (or a dimension of a
line) at `/metrics`. There's no framework generating this for you.

---

## Step 2 — The middleware: `internal/metrics/middleware.go`

### The status-capturing shim

`http.ResponseWriter` has no getter for the status code — you write it, you
don't read it back. Wrap it:

```go
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController (Go 1.20+) find the underlying
// Flusher/Hijacker through this wrapper. Nothing in this lab streams yet,
// but a wrapper that doesn't implement Unwrap silently breaks anything that
// tries to flush or hijack through it later — cheap to add now.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
```

Default `status` to `http.StatusOK` when constructing it — if the handler never
calls `WriteHeader` explicitly (writes straight to the body), Go's `net/http`
calls it implicitly with 200, and that implicit call still goes through your
`WriteHeader` override since `rec` *is* the `http.ResponseWriter` the handler
receives.

### The middleware itself

```go
func (m *Metrics) Instrument(routeFunc func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route := routeFunc(r)

			m.requestsInProgress.WithLabelValues(route).Inc()
			defer m.requestsInProgress.WithLabelValues(route).Dec()

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()

			defer func() {
				status := rec.status
				rp := recover()
				if rp != nil {
					status = http.StatusInternalServerError
				}

				elapsed := time.Since(start).Seconds()
				m.requestsTotal.WithLabelValues(r.Method, route, strconv.Itoa(status)).Inc()
				m.requestDuration.WithLabelValues(r.Method, route).Observe(elapsed)
				m.requestDurationSummary.WithLabelValues(r.Method, route).Observe(elapsed)

				if rp != nil {
					panic(rp) // re-panic after recording; net/http recovers per-request
				}
			}()

			next.ServeHTTP(rec, r)
		})
	}
}
```

**Why `routeFunc func(*http.Request) string` as a parameter, not a hardcoded
call to `api.routePattern`:** the `metrics` package shouldn't import `api` (it
would be backwards — `api` calls into `metrics`, not the other way round).
Passing the function in is plain dependency injection and makes this
middleware unit-testable with a fake `routeFunc`.

**Why the gauge Inc/Dec are separate `defer`s from the status/duration
recording:** the gauge must decrement no matter what — success, error, or
panic — the instant the request is done being handled, same as the duration
measurement. Two independent deferred statements, same guarantee, clearer to
read than one defer doing five things.

**Why `panic(rp)` at the end, not swallow it:** the handler panicking is real
information — you want it to still surface (`net/http`'s server recovers
per-connection and logs it) after your metric records `status=500` for it.
Swallowing it here would hide a bug from you the next time you look at
server logs.

---

## Step 3 — Wire it into the router — **read this before you touch `router.go`**

### The trap: don't nest a second `http.ServeMux` under the middleware

It's tempting to build an inner mux with all your routes, then wrap the
*whole thing* in one middleware call, and mount that on an outer mux next to
`/metrics`. **This silently breaks `routePattern`.** `http.ServeMux` clones the
request when it dispatches — `r.Pattern` is set on that clone, for that mux's
own match, at the moment *it* dispatches. If your middleware wraps an *entire
second mux* from the outside, the middleware runs before that inner mux ever
sees the request, so `r.Pattern` at that point is whatever the *outer* mux
matched (e.g. `"/"`, a catch-all) — not the inner route. I verified this with
a throwaway program: the outer-wrapping middleware printed `r.Pattern = "/"`
while the actual leaf handler printed the correct `"GET /users"`.

### The fix: one mux, wrap each route at registration

Instrument each handler *individually*, as the literal thing you register.
Then there's no second mux dispatch to race against — the middleware *is* the
leaf `http.ServeMux` dispatches to, so `r.Pattern` is already correct by the
time your middleware's `ServeHTTP` runs, before or after calling `next`.

```go
func NewRouter(cfg config.Config, m *metrics.Metrics) http.Handler {
	h := &Handlers{cfg: cfg, metrics: m}
	instrument := m.Instrument(routePattern)

	mux := http.NewServeMux()
	mux.Handle("GET /health", instrument(http.HandlerFunc(handleHealth)))
	mux.Handle("GET /users", instrument(http.HandlerFunc(handleUsers)))
	mux.Handle("GET /products", instrument(http.HandlerFunc(handleProducts)))
	mux.Handle("POST /orders", instrument(http.HandlerFunc(h.handleCreateOrder)))
	mux.Handle("GET /slow", instrument(http.HandlerFunc(h.handleSlow)))
	mux.Handle("GET /error", instrument(http.HandlerFunc(h.handleError)))

	// /metrics is deliberately NOT instrumented — scraping itself isn't an
	// app request you want mixed into your own RED numbers.
	mux.Handle("GET /metrics", promhttp.HandlerFor(m.Registry(), promhttp.HandlerOpts{}))

	return mux
}
```

`Handlers` gains a `metrics *metrics.Metrics` field; `main.go`'s call site
becomes `api.NewRouter(cfg, m)` where `m := metrics.New()`.

**Known, deliberate gap:** a request to an unmatched path (e.g. `/nope`) never
reaches any of these wrapped handlers — `ServeMux` serves its own
`NotFoundHandler` directly, uninstrumented, so **404s are not counted** by
`http_requests_total` right now. A real service usually wants that (dead
links, bots, broken clients). Fixing it means wrapping a catch-all `"/"` route
too — and *that's* exactly the case `routePattern`'s `"other"` fallback exists
for, since a genuinely unmatched request has no useful `r.Pattern`. Leaving
this out of Phase 2 on purpose; note it, don't fix it yet.

---

## Step 4 — Increment the business metric

In `handleCreateOrder`, right after the order is created:

```go
id := h.orderSeq.Add(1)
h.metrics.OrderCreated()
writeJSON(w, http.StatusCreated, orderResponse{...})
```

---

## Step 5 — Tests

`internal/metrics/middleware_test.go` — use
`github.com/prometheus/client_golang/prometheus/testutil` to read metric
values without going through HTTP exposition:

- **Success path**: fake handler writes 200; assert
  `requestsTotal{method="GET",route="/test",status="200"}` == 1,
  `requestsInProgress{route="/test"}` == 0 *after* the call returns.
- **Error path**: fake handler writes 500; assert the `status="500"` series.
- **Panic path**: fake handler panics; the test itself must `recover()` (the
  middleware re-panics on purpose); assert `status="500"` was still recorded
  and the in-progress gauge is still back to 0.
- **Route label is the template**: pass a `routeFunc` returning `/orders` for
  a request to `/orders?x=1`; assert the label is `/orders`, not the raw path.

`internal/api/router_test.go` — one integration check: hit `GET /metrics`,
assert the body contains `http_requests_total` and `http_request_duration_seconds_bucket`.

## Step 6 — `docs/02-metrics.md`

For each metric: name, type, labels, and **why it exists / what question it
answers**. Include:
- The Summary-vs-Histogram limitation, concretely (not just "summaries can't
  aggregate" — say what breaks: averaging two instances' P95 is not a real P95).
- The nested-mux `r.Pattern` trap from Step 3, so future-you doesn't
  reintroduce it.
- The 404-not-counted gap and why it's deliberately deferred.

---

## Definition of Done

```sh
make check
make run &
curl -s localhost:8080/metrics | grep -E '^(http_requests_total|http_request_duration_seconds|http_requests_in_progress|orders_created_total)'
curl -s localhost:8080/error?rate=1 >/dev/null   # force a 500
curl -s localhost:8080/metrics | grep 'status="500"'
curl -s -XPOST localhost:8080/orders -d '{"product_id":1,"qty":1}' >/dev/null
curl -s localhost:8080/metrics | grep orders_created_total
kill %1
```

- [ ] `/metrics` exposes all four RED-relevant metrics (+ the contrast Summary) with correct label names
- [ ] `/error` → `status="500"` series bumps; `/slow`, `/users`, etc. bump their own route's series
- [ ] `route` label is always a template (`/users`), never a raw path with query string
- [ ] A successful `POST /orders` bumps `orders_created_total`
- [ ] In-progress gauge returns to 0 after every request, including a panic
- [ ] `/metrics` itself has no `http_requests_total{route="/metrics"}` series
- [ ] Unit tests cover success / error / panic / route-templating
- [ ] `docs/02-metrics.md` written, including the nested-mux trap
- [ ] Committed, tagged `phase-2`

## Traps you should now be able to explain

- Why `/metrics` would pollute your own RED metrics if instrumented (scrape
  traffic isn't user traffic).
- Why the Summary can't answer "what's the P95 across all 3 replicas" and the
  Histogram can.
- Why a gauge, not a counter, for in-progress requests.
- Why wrapping a second nested mux from outside breaks `r.Pattern`.
