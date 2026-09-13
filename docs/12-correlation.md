# 12 — Correlation: three pillars, one click apart

## What "correlated" actually means here

Phases 2-11 built three pillars that each answer a different question, but
lived in three separate UIs: you'd read a metric, manually copy a
`trace_id`, paste it into Tempo, then copy a timestamp into Loki. Phase 12
removes the copy-pasting. Concretely, three pivots now work with **zero
manual tag edits**:

1. **Metric → Trace.** Hover a point on the P95 latency line in the RED
   dashboard; a small diamond (an **exemplar**) appears on some points. Click
   it → "View trace" → jumps straight to that exact request's trace in Tempo.
2. **Log → Trace.** A canonical request line in Loki has its `trace_id` field
   turned into a clickable link (a **derived field**) → jumps to the same
   trace.
3. **Trace → Log.** Any span in Tempo has a "Logs for this span" button →
   opens Loki filtered to exactly that trace's log lines.

## The identifier-consistency table

None of this works if the same service is called something different in
each pillar. Pin it down once:

| Pillar | Where the identifier lives | App value | Consumer value |
|---|---|---|---|
| Metrics (Prometheus scrape) | `job` label | `api` | `consumer` |
| Traces (OTel `InitTracer`, Phase 11) | `service.name` resource attribute | `app` | `consumer` |
| Logs (Alloy relabel, Phase 10) | `service` stream label | `app` | `consumer` |

Consumer is consistent end to end. The app service isn't: Prometheus's
`prometheus.yml` names its scrape job `api` (inherited from Phase 3, before
traces or logs existed), while `cmd/api/main.go`'s `obs.InitTracer(ctx, "app",
...)` and Alloy's `service` label both call it `app`. This is a real,
pre-existing mismatch — see the trap note below for why it doesn't break any
of the three pivots, and why it's left as-is rather than renamed. Every
Grafana correlation config in this phase (`tags: [{key: service.name, value:
service}]` in `tempo.yml`) depends on `service.name` matching the `service`
label — those two agree (`app`), so trace↔log correlation works with zero
per-panel tag overrides. Metric↔trace correlation goes through the
exemplar's own `trace_id` label instead, so it doesn't depend on this table
at all. `grafana/dashboards/correlation.json` has to know about the split
directly: it queries hand-written metrics with `job="api"` and Tempo's
span-metrics with `job="app"` — same service, two label values, because
that's what's actually on disk.

## How an exemplar gets attached

`internal/metrics/exemplar.go`'s `exemplarFor(ctx)` is the single place this
decision is made:

```go
func exemplarFor(ctx context.Context) prometheus.Labels {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() || !sc.IsSampled() {
		return nil
	}
	return prometheus.Labels{"trace_id": sc.TraceID().String()}
}
```

Both histogram observations that matter — HTTP request duration
(`internal/metrics/middleware.go`) and DB query duration
(`internal/metrics/metrics.go`'s `ObserveDBQuery`) — call this through a
shared `observeWithExemplar` helper instead of duplicating the sampled-check.
**No exemplar for an absent or unsampled span** — a `trace_id` that points at
a trace Tempo never recorded is worse than no link at all, since clicking it
just shows an empty search result with no explanation why.

Exemplars only survive the scrape in **OpenMetrics** format — the plain
Prometheus text exposition format (Phase 2) has no syntax for them. That's
why `EnableOpenMetrics: true` was added to both `/metrics` handlers
(`internal/api/router.go`, `cmd/consumer/main.go`) — a client asking for
`Accept: application/openmetrics-text` gets exemplars, one asking for plain
text still gets the same numbers, just without the trace pointer.

## Span-metrics vs. the hand-written RED

Tempo's `metrics_generator` derives `traces_spanmetrics_calls_total` and
`traces_spanmetrics_latency_bucket` straight from span data — the same
information Phase 2's `http_requests_total`/`http_request_duration_seconds`
carry, but computed by an entirely independent pipeline (Go middleware
incrementing a counter vs. Tempo aggregating spans it already stored).
`grafana/dashboards/correlation.json` puts both side by side. When they
track each other (within the sampling ratio — span-metrics only sees
*sampled* traces, so at `API_TRACE_SAMPLE_RATIO<1.0` it undercounts relative
to the hand-written counter, which sees every request), that's confirmation
the manual instrumentation is measuring the same reality a completely
different system also measures.

**Caught live while building this**: `traces_spanmetrics_calls_total` isn't
scoped to HTTP requests — Tempo's `metrics_generator` derives a metric point
for *every* span, so `{service="app"}` alone sums root HTTP spans together
with `db.orders.insert`, `cache.get products`, and `kafka.publish orders`
child spans. Without `span_kind="SPAN_KIND_SERVER"`, the "request rate"
panel actually read **6.8 req/s** against the hand-written metric's
**4.1 req/s** at the same instant — not because either number was wrong, but
because they were counting different things (all spans vs. HTTP requests
only). Adding the `span_kind` filter brought them to **8.36 vs. 8.27 req/s**
(~1% apart) under live load — the actual apples-to-apples comparison this
panel is supposed to make.

## The three-click walkthrough (also `make incident`)

```sh
make up
make load          # keep traffic flowing in another shell
make incident       # injects 800ms latency, waits for it to show up
```

1. Open the printed RED dashboard link. The P95 latency panel climbs.
2. Hover the p95 line where it's elevated — a diamond marker (exemplar)
   appears on the data point. Click it, then "View trace in Tempo".
3. In the trace waterfall, the root span's duration matches the injected
   latency. Click on it, then **"Logs for this span"**.
4. Loki opens, scoped to that trace's `trace_id`. The canonical `WARN
   "chaos injected failure"` line (Phase 6/9) is right there, sharing the
   exact same `trace_id` you started from on the metric.

Turn chaos back off when done:
```sh
curl -X POST localhost:8080/admin/chaos -d '{"enabled":false}'
```

## Traps to notice

- **`traces_spanmetrics_*` covers every span, not just HTTP requests.** Filter
  `span_kind="SPAN_KIND_SERVER"` when comparing it to a request-rate or
  request-latency metric, or child spans (DB queries, cache lookups, Kafka
  publishes) get silently folded into the count. See the live numbers above.
- **`job` vs `service.name` naming drift.** Prometheus's scrape config names
  the app's job `api` (`prometheus.yml`'s `job_name: api`, inherited from
  Phase 3), while traces and logs both call the same service `app`
  (`InitTracer(ctx, "app", ...)`, Alloy's `service` label). This is a
  pre-existing, cosmetic mismatch from earlier phases — it doesn't break any
  of the three pivots above (metric→trace goes through the exemplar's own
  `trace_id`, not `job`; trace→log matches on `service.name`↔`service`,
  which do agree) but it's the kind of drift that would matter the moment
  someone tried to correlate on `job` directly. Worth fixing if this were a
  real service; left as-is here since renaming `job: api` would touch every
  existing dashboard/alert built on that label since Phase 3.
- **Exemplars need OpenMetrics, not just the feature flag.** Enabling
  `--enable-feature=exemplar-storage` on Prometheus is necessary but not
  sufficient — the scrape itself must negotiate OpenMetrics
  (`EnableOpenMetrics: true` on the app's own `promhttp.Handler`), or there's
  nowhere in the wire format for the exemplar to ride.
- **An exemplar for an unsampled span is worse than none.** `exemplarFor`
  returns `nil` rather than a `trace_id` for a trace that was never actually
  recorded — a dead link erodes trust in every other link on the dashboard.
