# 10 — Loki: logs as the second pillar

## The incident metrics alone can't solve

The RED dashboard shows `/orders` P95 spiked and the error rate ticked up. It
does not tell you *which* order failed, *why* (bad product ID? DB timeout?
chaos toggled?), or let you find the other three log lines from that same
request. Metrics tell you something is wrong and roughly where; for the "what
exactly happened" question you need the actual log line, and a way to query
across every container's stdout without SSHing into each one. That's Loki.

## Push vs pull — the opposite of Prometheus

Prometheus **pulls**: it scrapes your app's `/metrics` on an interval. Loki
(via Alloy) **pushes**: Alloy reads every container's stdout through the
Docker API, batches it, and calls `loki.write` → `POST /loki/api/v1/push`.
The app still doesn't know either system exists — it just writes JSON to
stdout, same as Phase 9 built it. Alloy is the collector, the same role
`postgres_exporter` plays for Postgres in Phase 8: something else translates
your system's native output into the observability backend's wire format.

## Loki's label model == Prometheus's label model

A Loki **stream** is uniquely identified by its label set, exactly like a
Prometheus time series. The critical lab rule carries over unchanged: **stream
labels must be a small, bounded set.** `internal/obs`'s `request_id` is exactly
the kind of high-cardinality value Phase 2 already taught you never to put in
a Prometheus label — the same reasoning applies here, just applied to Loki
instead of Prometheus. This lab's Alloy pipeline (`alloy/config.alloy`)
promotes exactly three labels: `service`, `container`, `level`. Everything
else — `route`, `request_id`, `status`, `duration_ms` — stays inside the log
line's JSON body, parsed at query time with LogQL's `| json` stage, not at
ingest time as a label.

## LogQL

LogQL looks like PromQL because it's deliberately modeled on it: a label
matcher, then pipeline stages, then (optionally) wrapped in a range/aggregation
function.

```logql
{service="app"}                          # every log line from the app container
{service="app"} | json                    # parse the JSON body into fields
{service="app"} | json | level="ERROR"    # filter by a parsed field
```

## Metrics-from-logs

LogQL can produce a numeric time series from a log stream, the same shape as
`rate()`/`sum by (...)` in PromQL:

```logql
sum by (route) (rate({service="app"} | json | __error__="" [$__rate_interval]))
```

`__error__=""` filters out lines that failed to parse as JSON (so a malformed
line doesn't silently become its own broken series). This is the "Logs"
dashboard's second panel — a request-rate-by-route graph derived entirely from
log lines, computed independently of Phase 2's `http_requests_total` counter.
When the two agree, that's a small trust-building exercise: two completely
different pipelines (a Go middleware incrementing a counter vs. Alloy shipping
JSON to Loki) answering the same question the same way.

## The cardinality demo

**Method:** `alloy/config.alloy`'s `loki.process` stage promotes exactly
`service`, `container`, `level` to stream labels via `stage.label_keep`. To see
what happens if a high-cardinality field is promoted instead:

1. Baseline: `curl -s localhost:3100/metrics | grep loki_ingester_memory_streams`
   after a few minutes of `make load`.
2. Edit `alloy/config.alloy`: add `request_id = ""` to the `stage.labels`
   block and to `stage.label_keep`'s `values` list.
3. `docker compose restart alloy`, run `make load` again for the same
   duration, re-check `loki_ingester_memory_streams`.
4. Revert the edit, restart Alloy again.

Every unique `request_id` becomes its own stream once promoted to a label —
since `request_id` is minted fresh per request (Phase 9's
`crypto/rand`-backed 8-byte hex), stream count grows roughly 1:1 with request
count instead of staying flat at "one stream per (service, container, level)
combination." This is the Loki-specific version of Phase 2's route-templating
lesson: an unbounded label turns a handful of streams into a number that
scales with traffic, and Loki (like Prometheus) pays for that in memory and
query latency.

**Numbers from this environment:** not yet captured — this demo needs a
running `docker compose` stack (Docker daemon was not available while this
phase was authored). Run the four steps above against your own stack and
record the before/after `loki_ingester_memory_streams` values here.

## Retention and compaction

`limits_config.retention_period: 72h` plus `compactor.retention_enabled: true`
means Loki deletes chunks older than 3 days on its own schedule — appropriate
for a lab that isn't trying to be a permanent log archive. `delete_request_store:
filesystem` is required alongside `retention_enabled` when using filesystem
object storage; without it the compactor has nowhere to record pending
deletions and retention silently does nothing.

## Verifying the pipeline

```sh
make up
make load
curl -s 'http://localhost:3100/loki/api/v1/labels' | jq        # only service, container, level (+ internals)
curl -s 'http://localhost:3100/loki/api/v1/query_range?query=%7Bservice%3D%22app%22%7D' | jq '.data.result | length'
```

Alloy's own UI at `http://localhost:12345` shows each pipeline component
(`discovery.docker`, `loki.source.docker`, `loki.process`, `loki.write`) and
whether it's healthy — check this first if logs aren't arriving.
