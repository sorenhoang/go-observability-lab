# 09 — Structured logging

## Why not `fmt.Println`

A metric tells you *something* is wrong ("P95 on `/orders` is 2s"). A log tells
you *what exactly happened for one request* — which product ID, which error
message, which request. Structured logging means every line is a JSON object
with consistent keys, not a hand-written sentence — so it can be filtered,
aggregated, and (Phase 10) shipped to Loki and queried like a database.

## One canonical line per request

Every business, health, and admin request produces **exactly one** `"msg":
"request"` JSON line, emitted after the handler returns:

```json
{"time":"...","level":"INFO","msg":"request","method":"GET","route":"/users","request_id":"a1b2c3d4e5f6a7b8","status":200,"duration_ms":1,"bytes_out":142}
```

This is the "canonical log line" pattern: one line per unit of work, wide
(many fields), rather than a scattered trail of `log.Println` calls at
different points in the handler. It's cheap to grep, cheap to parse, and
gives you request/response shape without reading the code.

Severity keys off the response status: `< 400` → `INFO`, `400–499` → `WARN`,
`>= 500` → `ERROR`. `/metrics` itself is excluded — same reasoning as
Phase 2's RED metrics: scraping infrastructure isn't a "request" worth its own
canonical line.

## request_id — the correlation key

Every request gets an 8-random-byte hex `request_id`, attached to the logger
for the lifetime of that request via `context.Context`. Any log line emitted
while handling the request — the canonical line, a cache-miss warning, a
chaos-injected failure — carries the same `request_id`, so you can grep one ID
and see everything that happened for that request. Phase 11 adds `trace_id` /
`span_id` alongside it once tracing exists; this phase lays the groundwork by
making trace ID injection a first-class (if currently empty) feature of the
JSON handler.

## The middleware stack

```
PanicGuard( TraceHTTP( RequestLogger( Instrument( Chaos( handler ) ) ) ) )
```

- **PanicGuard** (outermost) — recovers a handler panic, logs the stack at
  `ERROR`, writes a clean `500`. It never re-panics, so nothing above it (or
  the Go HTTP server) ever sees a panic escape a handler.
- **TraceHTTP** — a no-op pass-through in this phase. Phase 11 replaces it
  with real span creation; the seam exists now so the order doesn't reshuffle
  later.
- **RequestLogger** — mints `request_id`, attaches the per-request logger to
  the context, emits the canonical line.
- **Instrument** — unchanged from Phase 2: RED metrics.
- **Chaos** — unchanged from Phase 6: fault injection, now with its own
  `WARN` line when it forces a failure.

`/health` skips `Chaos` (it must stay honest) and `TraceHTTP` (nothing to
trace on a probe). `/metrics` is untouched — raw `promhttp`, no middleware at
all, so scraping never pollutes application logs.

## What never gets logged

Request bodies, the `Authorization` header, and any `?token=` query value.
`RequestLogger` only ever reads `r.Method`, the matched route template, and
the final status/byte count — never the body or headers. This matters
because `/admin/chaos` and friends are guarded by a bearer token or
`?token=` query param; a naive "log everything" middleware would leak it into
every log aggregator downstream.

## `API_LOG_LEVEL`

Default `info`. Set to `warn` in a noisy environment to drop the canonical
`INFO` line for every 2xx/3xx request and keep only `WARN`/`ERROR` — useful
once Phase 10 ships these logs to Loki and volume becomes a cost concern.
