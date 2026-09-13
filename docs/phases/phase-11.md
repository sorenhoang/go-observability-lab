# Phase 11 — Distributed tracing

**Status: DONE.** Verified live against a real `docker compose` stack: a
`POST /orders` request carrying a hand-crafted `traceparent` produced a real
5-span, 2-service trace in Tempo — `app: /orders` → `db.products.exists` +
`db.orders.insert` + `kafka.publish orders` → `consumer: consume orders` —
confirmed both via Tempo's HTTP API and visually in Grafana Explore's trace
waterfall.

## Objective

Metrics say something is wrong; logs say what happened for one request; a
trace says *where the time went*, across process boundaries. This phase wires
a real distributed trace: `POST /orders` in the app, a Kafka publish, and a
`consume orders` span in a completely different process (`cmd/consumer`),
all one trace in Tempo.

## Concepts to internalize

- **The goroutine-span-lifetime trap.** A span started inside a detached
  `go func()` has no guaranteed live parent by the time it runs. Start it
  synchronously on the request goroutine, end it inside the goroutine once
  the async work actually finishes. See `docs/11-tracing.md` for the full
  walkthrough — this is the one mistake this phase is built to teach you not
  to make.
- **W3C `traceparent` across a non-HTTP boundary.** The OTel HTTP propagator
  only applies to HTTP headers. Kafka messages need the same format
  hand-rolled onto `kafka.Message.Headers` and parsed back out on the
  consumer side — `internal/obs/propagation.go` (Task 4) is exactly that.
- **`ParentBased` sampling.** Sample the root, honor that decision at every
  child and every service the trace crosses — never re-roll the dice per hop,
  or a partially-sampled trace becomes useless.
- **OTLP as a swappable wire format.** Same lesson as Prometheus's exposition
  format and Loki's push API: the app speaks one protocol (OTLP/gRPC) and
  doesn't know or care whether Tempo, Jaeger, or anything else is listening.

## What was built

### `internal/obs` consumers (Task 4 primitives, now wired in)

- `internal/api/router.go` — no code change needed: `TraceHTTP` was already
  wired to `obs.TraceHTTP(routePattern)` since Task 2, when it was a no-op
  stub. Task 4 replaced the function *body*; the router's call site never
  changed. Order confirmed: `PanicGuard → TraceHTTP → RequestLogger →
  Instrument → Chaos`.
- `internal/store/store.go` — `timed()` wraps every query in a `db.<name>`
  child span (`db.system=postgresql`, `db.operation=<name>`), records errors.
- `internal/cache/cache.go` — `Products()` wraps its whole body in
  `cache.get products`, `cache.hit` (bool) attribute; `load()` runs inside
  the span's context, so a miss's `db.products.list` nests underneath.
- `internal/events/producer.go` — `PublishOrder` starts `kafka.publish
  orders` synchronously (`SpanKindProducer`), injects `traceparent` into the
  Kafka header, ends the span inside the publish goroutine. The `*kafka.Writer`
  field became a `kafkaWriter` interface so tests can inject a fake instead of
  dialing real Kafka — `PublishOrder`'s own signature never changed.
- `cmd/consumer/main.go` — `consumeSpanContext(headers)` parses the
  `traceparent` header back into a `trace.SpanContext`; each message starts
  `consume orders` (`SpanKindConsumer`) as its child when present, or a fresh
  root when absent (never fails the message over missing tracing data).
- `cmd/api/main.go`, `cmd/consumer/main.go` — both call `obs.InitTracer` at
  startup and defer its shutdown.

### Config

```env
API_OTLP_ENDPOINT=tempo:4317
API_TRACE_SAMPLE_RATIO=1.0
CONSUMER_OTLP_ENDPOINT=tempo:4317
CONSUMER_TRACE_SAMPLE_RATIO=1.0
```

Empty `*_OTLP_ENDPOINT` disables export entirely — spans still create (so
logging's trace ID injection keeps working), nothing is sent anywhere.

### Infra

- `tempo/tempo-config.yml` — single-binary, local disk storage, OTLP
  receiver (4317 gRPC / 4318 HTTP), `metrics_generator` (span-metrics +
  service-graphs) remote-writing into Prometheus.
- `docker-compose.yml` — `tempo` service; `app`/`consumer` get OTLP env;
  `prometheus`'s command gains `--web.enable-remote-write-receiver` so
  Tempo's metrics-generator has somewhere to write to; `grafana depends_on: [tempo]`.
- `grafana/provisioning/datasources/tempo.yml` — `serviceMap.datasourceUid:
  prometheus`, `nodeGraph.enabled: true` (trace↔log↔metric correlation is
  Task 6/Phase 12).
- `loadgen/script.js`, `spike.js` — every request originates a fresh W3C
  `traceparent` header (see `docs/11-tracing.md` for why not
  `k6/experimental/tracing`).

## Definition of Done

- [x] `go test ./...` green
- [x] `make check-config` includes Tempo, verified live: `-config.verify=true` → clean exit
- [x] Unit test proves the end-to-end chain: a `kafka.publish orders` span
      that's a child of the request's root span, carrying a header that
      round-trips through `ParseTraceparent`, and a consumer-side
      `consumeSpanContext` that reconstructs the same trace ID from that
      header
- [x] `/health`, `/metrics` produce no spans (not wrapped in `TraceHTTP`/`plain` skips it)
- [x] `API_OTLP_ENDPOINT=` (empty) → `InitTracer` returns a working no-op,
      app/consumer start normally (unit-tested in Task 4)
- [x] `docs/11-tracing.md` covers the goroutine-span trap, sampling, and the
      Jaeger swap
- [x] Confirmed live: `make up`, a `POST /orders` with a hand-crafted
      `traceparent` header, then that exact trace ID pasted into Grafana
      Explore's TraceQL box. Real waterfall, 5 spans, 2 services:
      `app: /orders (1.08ms)` → `db.products.exists (307µs)` +
      `db.orders.insert (631µs)` + `kafka.publish orders (15.16ms)` →
      `consumer: consume orders (52.12ms)`, with `consume orders`'s
      `parentSpanId` matching `kafka.publish orders`'s `spanId` exactly
      (checked via Tempo's raw `/api/traces/{id}` JSON, not just the UI)

## Traps to notice

- **Starting the publish span inside `go func()`.** Covered at length in
  `docs/11-tracing.md` — the single most important lesson this phase teaches.
- **Tempo's config schema is not stable across major versions.** 3.0 is a
  different architecture (Kafka-backed ingest, no more `ingester`/`compactor`
  blocks) — verified live that a 3.0.3 config using the 2.x shape fails to
  parse. Pinned `2.10.8` instead. Always verify a config against the *exact*
  pinned tag, not just "the docs for the major version."
- **A missing/malformed `traceparent` header must degrade, not fail.** The
  consumer still processes the Kafka message and just starts a disconnected
  trace — never lose a business event because a trace header was absent.
- **`k6/experimental/tracing` is gone.** Confirmed against the pinned image;
  don't reach for it without checking first.
