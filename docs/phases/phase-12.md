# Phase 12 — Correlation

**Status: DONE.** Exemplars wired end to end (`go test` proves the histogram
carries a `trace_id` exemplar in OpenMetrics format for a sampled request),
all three Grafana datasources cross-linked, `correlation.json` compares the
hand-written RED metrics against Tempo's span-metrics, `make incident`
walks the full metric→trace→log pivot.

## Objective

Phases 2-11 built three pillars in three separate UIs. This phase removes
the copy-pasting between them: hover an exemplar on a latency graph, land on
the exact trace; read a log line, land on its trace; read a span, land on
its logs. Proven with one scripted incident, not just config that looks
right on paper.

## Concepts to internalize

- **Exemplars** — a metric can carry a per-observation label pointing at a
  specific trace, but only for a *sampled* observation (an unresolvable
  `trace_id` is worse than none) and only over **OpenMetrics**, not the
  plain Prometheus text format.
- **Derived fields** (Loki) and **`tracesToLogsV2`/`tracesToMetrics`**
  (Tempo) — Grafana-side datasource config that turns a regex match or a
  span attribute into a cross-datasource link, no per-dashboard wiring.
- **Identifier consistency** — every cross-pillar link depends on the same
  service being named the same thing in each pillar's label. This lab has
  one real mismatch (Prometheus `job=api` vs. traces/logs `app`) that
  survives because none of the three pivots actually route through `job`.
- **Span-metrics as a second, independent RED** — Tempo's `metrics_generator`
  derives request-rate/latency from spans alone; comparing it against the
  hand-written Phase 2 metrics is a trust-building exercise, not a
  replacement for either.

## What was built

- `internal/metrics/exemplar.go` — `exemplarFor(ctx)`, the one place the
  sampled-check lives; `observeWithExemplar` shared by the HTTP duration
  histogram (`middleware.go`) and `ObserveDBQuery` (`metrics.go`, which
  gained a `ctx` parameter for this).
- `internal/api/router.go`, `cmd/consumer/main.go` — `/metrics` now serves
  `EnableOpenMetrics: true`.
- `prometheus/prometheus.yml` — `storage.exemplars.max_exemplars`, a new
  `tempo` scrape job. `docker-compose.yml`'s Prometheus command gains
  `--enable-feature=exemplar-storage`.
- Three Grafana datasources extended: `prometheus.yml`
  (`exemplarTraceIdDestinations` → tempo), `loki.yml` (`derivedFields` →
  tempo, matching `"trace_id":"(\w+)"`), `tempo.yml` (`tracesToLogsV2` →
  loki, `tracesToMetrics` → prometheus's `traces_spanmetrics_calls_total`).
- `grafana/dashboards/red.json` — the P95 target gains `"exemplar": true`.
- `grafana/dashboards/correlation.json` — new dashboard, 4 panels: hand-written
  vs. span-metrics request rate, hand-written vs. span-metrics P95.
- `scripts/incident.sh` + `make incident` — injects 800ms chaos latency,
  waits for a few scrapes, prints the dashboard link and the click sequence.

## Definition of Done

- [x] `go test ./...` green, including
      `TestDurationHistogramCarriesExemplarForSampledRequest` (asserts a real
      `# {trace_id="..."}` exemplar comment in OpenMetrics output)
- [x] `curl -H 'Accept: application/openmetrics-text' localhost:8080/metrics`
      → exemplar present after traced load
- [x] All three Grafana pivots configured with no manual per-panel tag edits
      (verified live: real exemplar → trace, real log `TraceID` link → trace,
      real span → "Logs for this span")
- [x] `traces_spanmetrics_calls_total` present in Prometheus once traced
      traffic flows; `correlation.json` provisioned
- [x] `make incident` + `docs/12-correlation.md` reaches the injected-failure
      span and its log line, same `trace_id`
- [x] `make check-config` green (Prometheus config + Tempo + Loki all
      verified against the real binaries)
- [x] `make test` green with no infra running
- [x] README checklist fully ticked

## Traps to notice

- **`traces_spanmetrics_*` isn't scoped to HTTP requests.** Tempo's
  `metrics_generator` derives a metric point for every span — DB queries,
  cache lookups, the Kafka publish, not just the root HTTP span. Caught live:
  without `span_kind="SPAN_KIND_SERVER"`, `correlation.json`'s request-rate
  panel read 6.8 req/s against the hand-written metric's 4.1 req/s at the
  same instant. Adding the filter brought them to 8.36 vs. 8.27 (~1% apart)
  under load — the real comparison.
- **`job=api` vs. `service.name=app`/`service=app`.** A real, pre-existing
  naming mismatch from Phase 3 — Prometheus's own scrape names the app's job
  `api`, while traces (`service.name`) and Tempo's derived span-metrics
  (`service` label) both call it `app`. Doesn't break any of the three
  pivots (none route through `job` directly), but `correlation.json`'s
  request-rate panels use different label values on each side
  (`job="api"` for the hand-written metric, `service="app"` for
  span-metrics) because that's what's actually on disk — and neither of
  those two labels is even spelled the same way (`job` vs. `service`).
  Don't "fix" one query to match the other without checking which label the
  underlying series actually carries.
- **The feature flag alone doesn't produce exemplars.** Needs
  `--enable-feature=exemplar-storage` *and* `EnableOpenMetrics: true` on the
  scraped `/metrics` endpoint *and* a request with a sampled span in its
  context. Missing any one of the three silently produces a normal
  histogram with no exemplar — no error, just nothing to click.
- **Span-metrics undercounts relative to hand-written metrics whenever
  `API_TRACE_SAMPLE_RATIO < 1.0`.** It only sees sampled traces; the
  hand-written counter sees every request. Don't read a gap between the two
  `correlation.json` panels as a bug before checking the sample ratio.
