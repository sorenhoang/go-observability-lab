# 11 — Distributed tracing

## The question metrics and logs can't answer alone

`/orders`'s P95 spiked (metrics). The canonical log line for one slow request
shows `duration_ms: 1800` (logs). But *where* did those 1800ms go — the
database insert, the cache lookup, the Kafka publish? A trace answers that:
one request, broken into a tree of timed spans, each one attributable to a
specific piece of code.

## The three pillars, one request

```
POST /orders                              (root span, app)
├── cache.get products                    (only on GET /products)
├── db.orders.insert                      (app, Postgres)
└── kafka.publish orders                  (app, async, ends when the write completes)
        │  traceparent header
        ▼
    consume orders                        (consumer service, a different process)
```

The root span and every child stay inside one process's call stack via
`context.Context` — that part is "free" once `TraceHTTP` starts the root
span. The interesting part is the two places a trace has to survive crossing
a boundary the plain `context.Context` chain can't reach across:

## The goroutine-span-lifetime trap

`PublishOrder` fires the actual Kafka write in a `go func()` so the HTTP
response doesn't wait on it. The tempting-but-wrong move is to start the
`kafka.publish orders` span *inside* that goroutine:

```go
// WRONG — the span has no valid parent by the time this runs
go func() {
    _, span := obs.Tracer().Start(ctx, "kafka.publish orders")
    defer span.End()
    ...
}()
```

By the time the goroutine actually runs, the HTTP handler may have already
returned and `TraceHTTP`'s root span may have already ended — starting a
*new* span at that point either attaches to a span that's already closed
(most SDKs disallow this and silently drop the relationship) or, worse,
starts a brand-new orphan trace with no connection to the request that
triggered it. Either way, the waterfall breaks: Tempo shows `kafka.publish
orders` as its own disconnected trace instead of nested under `POST /orders`.

The fix (`internal/events/producer.go`): start the span **synchronously**,
before the goroutine, while `ctx` still definitely has a live parent:

```go
ctx, span := obs.Tracer().Start(ctx, "kafka.publish orders", trace.WithSpanKind(trace.SpanKindProducer))
traceparent := obs.FormatTraceparent(span.SpanContext())

go func() {
    defer span.End()               // ends when the write actually finishes
    writeCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
    defer cancel()
    // ... p.writer.WriteMessages(writeCtx, kafka.Message{Headers: [...traceparent...]})
}()
```

`context.WithoutCancel(ctx)` detaches the goroutine from the request's
cancellation (so a client disconnect doesn't abort an in-flight publish that
already has the data), but it still carries `span` as its active span — the
span object itself was captured by the closure, not looked up from context,
so it doesn't matter that the *cancellation* is severed.

## Crossing the Kafka boundary

There's no HTTP request between the producer and the consumer, so the OTel
HTTP propagator (`propagation.TraceContext`) doesn't apply — Kafka message
headers are a different transport. `internal/obs/propagation.go` (Task 4)
hand-rolls the same W3C `traceparent` format the propagator would have used:

- **Producer**: `obs.FormatTraceparent(span.SpanContext())` → `kafka.Message.Headers["traceparent"]`.
- **Consumer** (`cmd/consumer/main.go`): `consumeSpanContext(msg.Headers)` parses
  that header back into a `trace.SpanContext`, which becomes the parent via
  `trace.ContextWithRemoteSpanContext` for the `consume orders` span.

If the header is missing or malformed, the consumer still processes the
message — it just starts a fresh, disconnected trace instead of erroring.
Never fail a business operation because tracing plumbing was incomplete.

## Sampling

Both sides use `sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))`.
`ParentBased` means: if a span already has a sampled parent (the common case —
k6 originates a trace, the app's root span honors it, the producer span
honors the app's root, the consumer honors the producer's), keep that
decision consistent all the way down. Only a *new* root trace (no parent at
all) actually rolls the `TraceIDRatioBased` dice. This is why the consumer
"honoring the sampled flag from the incoming traceparent" isn't extra
work — it's just what `ParentBased` already does once the remote span
context (carrying that flag) is set as the parent.

`API_TRACE_SAMPLE_RATIO=1.0` (default) samples everything — fine for a lab.
In production you'd drop this to something like `0.05`–`0.1` once trace
volume becomes a storage/cost concern, the same tradeoff `API_ERROR_RATE` and
friends teach at a smaller scale.

## k6 and traceparent

The plan's preferred approach was `k6/experimental/tracing`'s
`instrumentHTTP({ propagator: 'w3c' })`. Confirmed against the pinned
`grafana/k6:2.2.0` image, that module has been **removed** from k6 — its
replacement is a pure-JS jslib fetched from a URL
(`http-instrumentation-tempo`), which would add an external network
dependency this load generator doesn't otherwise have just to mint a header.
`loadgen/script.js` and `spike.js` instead use the task's documented
fallback: a tiny `traceparent()` helper generating a fresh random W3C header
per request, attached via `{ headers: { traceparent: ... } }` — no script
structure change, no new dependency.

## Swapping Tempo for Jaeger

Nothing in this app is Tempo-specific. `internal/obs.InitTracer` speaks
plain OTLP/gRPC — any OTLP-compatible backend works by changing one thing:
`API_OTLP_ENDPOINT` (and `CONSUMER_OTLP_ENDPOINT`) to point at Jaeger's OTLP
receiver instead (Jaeger has accepted OTLP natively since ~1.35). No code
change, no new dependency, no `docker-compose.yml` change beyond swapping the
`tempo` service for a `jaeger` one. This is the same "the collector is
swappable, the app doesn't know which one it's talking to" property Loki has
via Alloy and Prometheus has via the pull model — OTLP is doing for traces
what the exposition format does for metrics.

## Why not `otelhttp`/`otelsql`/`otelkafka`

This lab hand-writes every span deliberately (`docs/00-intro.md`'s
philosophy: "you should be able to explain what every metric/span does").
The `go.opentelemetry.io/contrib/instrumentation/...` packages exist and
would remove most of this code — that's exactly why they're banned here.
Once you've hand-wired one HTTP span, one DB span, and one Kafka
producer/consumer span pair, you understand what any auto-instrumentation
library is actually doing under the hood, and you can judge whether it's
doing the right thing for your service.

## Tempo version note

The plan called for Tempo's 3.x line, but 3.0 is a genuine architectural
rewrite (ingesters and the compactor are replaced by block-builders,
live-stores, and a Kafka-backed ingest pipeline — verified against the real
image: a 3.0.3 config using the classic `ingester`/`compactor` blocks fails
to parse with `field ingester not found in type app.Config`). That's a
production-scale answer to a lab-scale problem. This lab pins
`grafana/tempo:2.10.8` — the latest patch of the still-maintained 2.x line,
which keeps the simple single-binary `ingester`/`compactor`/local-storage
shape the rest of this lab's services use, verified live with
`-config.verify=true`.
