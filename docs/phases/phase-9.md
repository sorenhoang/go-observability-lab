# Phase 9 — Structured logging

**Status: DONE.** JSON logging in all three binaries, canonical one-line-per-request
logging, request_id correlation, no new infrastructure.

## Objective

Add the second observability pillar — logs — without touching infrastructure.
Every request now produces one structured JSON line with a `request_id` that
ties it to any warning emitted while handling it (a chaos-injected failure, a
cache miss). This is pure application-code work: no Loki, no shipping, no new
container. Phase 10 adds the shipping pipeline; this phase makes the app worth
shipping logs *from*.

## Concepts to internalize

- **The canonical log line** — one wide, structured line per request, emitted
  after the handler returns, instead of scattered `log.Println` calls. Cheap
  to grep, cheap to parse, gives you the request's shape without reading code.
- **Context-carried correlation ID** — `request_id` travels on
  `context.Context`, not as a function parameter threaded through every call.
  Any code with access to the request's `ctx` can log with the same ID by
  pulling the logger back off it (`obs.LoggerFrom(ctx)`).
- **A decorating `slog.Handler`** — wrapping `slog.NewJSONHandler` so every
  record picks up `trace_id`/`span_id` from context (empty today, populated in
  Phase 11) is the same pattern Prometheus's own client uses for exemplars:
  decorate once at the edge, never remember at every call site.
- **Redaction by construction, not by filtering** — `RequestLogger` never
  reads the request body, headers, or query string. There's no secret-scrubber
  regex to keep in sync with every new field; the middleware simply has
  nothing to leak.
- **Middleware order matters for what gets recorded** — `PanicGuard` outermost
  means a panic always gets a clean `500` and a stack trace, from any handler,
  without every handler needing its own recover.

## What was built

### 1. `internal/obs` — pure-Go primitives (Task 1)

`context.go` (ctx-carried logger + trace IDs), `logging.go` (`NewHandler`,
trace-ID-injecting JSON handler), `middleware.go` (`PanicGuard`,
`RequestLogger`, and a `TraceHTTP` no-op stub for Phase 11). Stdlib only — no
new dependency, and `internal/obs` cannot import `internal/metrics` or
`internal/api` (prevents a cycle once Phase 12 adds exemplars).

### 2. Wired into the app (Task 2)

- `cmd/api/main.go`, `cmd/consumer/main.go`, `cmd/sink/main.go` — swapped
  `slog.NewTextHandler` for `obs.NewHandler`; all three binaries now emit JSON.
- `internal/api/router.go` — `NewRouter` takes a `*slog.Logger`; every route
  now goes through `PanicGuard(TraceHTTP(RequestLogger(Instrument(...))))`
  (business routes add `Chaos`; `/health` skips `Chaos`+`TraceHTTP`; `/metrics`
  is untouched raw `promhttp`).
- `internal/api/chaos.go`, `handlers.go` — the chaos-injected-failure branch
  and the `/error` simulated-failure branch each log a `WARN` with the
  request's route, using `obs.LoggerFrom(r.Context())`.
- `internal/cache/cache.go`, `internal/events/producer.go` — every
  `slog.Warn` call became `obs.LoggerFrom(ctx).Warn`, so a cache-miss or a
  failed Kafka publish carries the same `request_id` as the request that
  triggered it.
- `internal/config` — `API_LOG_LEVEL` (default `info`), `Config.SlogLevel()`.

### 3. Config

```env
API_LOG_LEVEL=info   # debug | info | warn | error
```

## Definition of Done

- [x] `go test ./...` all green
- [x] `make run` emits JSON; startup lines preserved
- [x] one `msg="request"` line per business/health/admin request; none for `/metrics`
- [x] a chaos-injected 500 → a `WARN` line sharing the request's `request_id`
- [x] `API_LOG_LEVEL=warn` suppresses INFO request lines, keeps WARN/ERROR
- [x] `go vet ./...` clean, `gofmt -l .` empty
- [x] Tagged `phase-9`

## Traps to notice

- **A middleware that recovers must decide whether to re-panic.**
  `metrics.Instrument` already recovers-and-re-panics so its own RED metrics
  stay accurate even on a crash; `PanicGuard` recovers and does **not**
  re-panic, so it must sit outside `Instrument` in the chain, not replace it.
- **`context.WithoutCancel` in the Kafka publish goroutine** still needs the
  request's logger — captured into a local variable *before* the `go func()`,
  since the goroutine's context is deliberately decoupled from the request
  context and calling `obs.LoggerFrom` inside the goroutine on the *original*
  `ctx` would still work, but capturing it up front makes the dependency
  obvious at a glance.
- **`/metrics` must stay silent.** It's the one endpoint that's *supposed* to
  be hit every few seconds forever; wrapping it in `RequestLogger` would flood
  the log stream with lines nobody reads.
