# Roadmap

Nine phases. Each is independently runnable and tagged `phase-N` in git. Each
phase adds one small cluster of concepts. Do not skip ahead — Phase 5's panels
are lifted straight from Phase 4's queries, which need Phase 3's live data.

## Stack decisions (locked)

| Decision | Choice | Why |
|----------|--------|-----|
| Language / router | Go 1.26, stdlib `net/http` + `ServeMux` | No framework, so the middleware seam stays visible |
| Instrumentation style | Explicit hand-written middleware | You learn the mechanism, not an abstraction |
| Metrics client | `prometheus/client_golang` | The reference Go client |
| Load generator | `grafana/k6` container | Scriptable; ramping scenarios needed for P6 spikes |
| Alert sink | Local webhook receiver container | Teaches the chain without a real Slack/email account |
| Data layer (P1–P7) | In-memory fakes | Keeps the app trivial; a real DB only lands in P8 |
| Orchestration | Docker Compose | No Kubernetes |

---

## Phase 0 — Repository bootstrap

1. **Objective** — Clean Go skeleton that builds and runs; docs structure ready.
2. **Concepts** — `cmd/` vs `internal/` layout; graceful shutdown; the three
   pillars (metrics / logs / traces) and why this lab is metrics-only.
3. **Components** — `go.mod`, `cmd/api/main.go`, `internal/api`, `internal/config`,
   `Makefile`, `.editorconfig`, `.golangci.yml`, `docs/`.
4. **Metrics** — none.
5. **PromQL** — none.
6. **Dashboards** — none.
7. **Repo changes** — the skeleton above; README rewritten with the phase checklist.
8. **Definition of Done**
   - `make run` serves `GET /health` → `{"status":"ok"}` (JSON content-type)
   - `make build` produces `./bin/api`; `make test` clean; `go vet ./...` clean
   - Ctrl-C shuts down cleanly (no panic, no deadline-exceeded)
   - README has the full checklist; `docs/` has intro, glossary, roadmap
   - Tagged `phase-0`

---

## Phase 1 — Simple Go API

1. **Objective** — The intentionally boring API. No metrics yet, so Phase 2's diff
   is pure instrumentation.
2. **Concepts** — `net/http`, `ServeMux` method+pattern routing, handlers,
   request validation, route **templates** vs raw paths.
3. **Components** — handlers for `GET /health`, `GET /users`, `GET /products`,
   `POST /orders`, `GET /slow`, `GET /error`. In-memory fake data.
4. **Metrics** — none.
5. **PromQL** — none.
6. **Dashboards** — none.
7. **Repo changes** — `internal/api/router.go` (extended), `internal/api/handlers.go`,
   `internal/api/handlers_test.go`, `docs/01-api.md`.
8. **Definition of Done**
   - All six routes reachable; unknown path → 404, wrong method → 405
   - `POST /orders` validates: qty > 0, product exists; 201 / 400 / 404 as appropriate
   - `/slow` sleeps a random 50 ms–2 s (`?max_ms=` override); `/error` returns 500
     with configurable probability (default 0.3, `?rate=` override)
   - A `routePattern(*http.Request) string` helper returns the template (`/users`),
     never the raw path — this is the foundation for low-cardinality labels
   - Table tests cover every handler branch
   - Tagged `phase-1`

---

## Phase 2 — Prometheus instrumentation (the core learning phase)

1. **Objective** — Expose RED metrics by hand.
2. **Concepts** — **Counter, Gauge, Histogram, Summary, Labels, the registry, the
   `/metrics` exposition format.** Go slow here.
3. **Components** — `prometheus/client_golang`, a metrics middleware, a
   status-capturing `responseWriter` shim, `promhttp` handler on `/metrics`.
4. **Metrics introduced**
   | Metric | Type | Why it exists |
   |--------|------|---------------|
   | `http_requests_total{method,route,status}` | Counter | Rate **and** error ratio both derive from this — the R and E of RED |
   | `http_request_duration_seconds{method,route}` | Histogram | The D — lets PromQL compute P50/95/99 aggregated across instances |
   | `http_requests_in_progress{route}` | Gauge | Saturation signal; catches request pileups a counter smooths over |
   | `orders_created_total` | Counter | First business metric — proves metrics aren't only HTTP |
   | Go runtime + process collectors | — | goroutines, GC, heap, CPU — free, and they matter in P6 |
5. **PromQL** — none yet; eyeball the raw series at `:8080/metrics`.
6. **Dashboards** — none.
7. **Repo changes** — `internal/metrics/metrics.go`, `internal/metrics/middleware.go`,
   `internal/metrics/middleware_test.go`, `docs/02-metrics.md`.
8. **Definition of Done**
   - `/metrics` shows all four metrics in exposition format with correct label names
   - Hitting `/error` bumps `http_requests_total{status="500"}`
   - `route` label uses the template; unmatched requests fall back to `"other"`
   - A handler panic still records `status="500"` and decrements the in-progress gauge
   - `/metrics` itself is **not** instrumented by the RED middleware
   - Tagged `phase-2`

---

## Phase 3 — Prometheus server, scraping, load generator

1. **Objective** — Prometheus scrapes the app; traffic flows continuously.
2. **Concepts** — **Scraping, targets, `scrape_interval`, the pull model, `up`,
   static service discovery**, scrape interval vs rate window.
3. **Components** — `Dockerfile` (multi-stage static), `docker-compose.yml` with
   `app` + `prometheus` + `k6`, `prometheus/prometheus.yml`, `loadgen/script.js`.
4. **Metrics** — `up{job="api"}` (synthesized by Prometheus), `scrape_duration_seconds`.
5. **PromQL** (Prometheus expression browser) — `up`, `http_requests_total`,
   `rate(http_requests_total[1m])`, `scrape_duration_seconds`.
6. **Dashboards** — none (Prometheus UI only).
7. **Repo changes** — `Dockerfile`, `docker-compose.yml`, `prometheus/prometheus.yml`,
   `loadgen/script.js`, `Makefile` (`up`/`down`/`load`), `docs/03-scraping.md`.
8. **Definition of Done**
   - `docker compose up` → Targets page shows `api` **UP**
   - `make load` drives continuous traffic; `rate(http_requests_total[1m])` is non-zero and stable
   - Stopping `app` flips `up` to 0 within ~15 s
   - Tagged `phase-3`

---

## Phase 4 — PromQL practice

1. **Objective** — Build (and understand) the queries you will paste into Grafana next.
2. **Concepts** — **Instant vs range vectors; `rate` vs `irate` vs `increase`;
   aggregation (`sum by`); `histogram_quantile`; label matchers; why `rate()` goes
   inside `sum()`.**
3. **Components** — none new; `docs/promql-cheatsheet.md` and
   `docs/04-promql-exercises.md` filled by doing.
4. **Metrics** — existing.
5. **PromQL to master**
   | Question | Query |
   |----------|-------|
   | Requests/sec (total) | `sum(rate(http_requests_total[5m]))` |
   | Requests/sec by endpoint | `sum by (route) (rate(http_requests_total[5m]))` |
   | Requests/sec by status | `sum by (status) (rate(http_requests_total[5m]))` |
   | Error ratio | `sum(rate(http_requests_total{status=~"5.."}[5m])) / sum(rate(http_requests_total[5m]))` |
   | P95 latency (global) | `histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket[5m])))` |
   | P95 latency per route | `histogram_quantile(0.95, sum by (le, route) (rate(http_request_duration_seconds_bucket[5m])))` |
   | In-flight requests | `sum(http_requests_in_progress)` |
   | Availability (5 m) | `avg_over_time(up{job="api"}[5m])` |
6. **Dashboards** — none.
7. **Repo changes** — `docs/promql-cheatsheet.md`, `docs/04-promql-exercises.md`.
8. **Definition of Done**
   - Every query in the cheatsheet returns data against live loadgen
   - You can write "error rate of `/orders` over 5 m" unaided and explain the
     `sum(rate(...))` ordering
   - Tagged `phase-4`

---

## Phase 5 — Grafana RED dashboard

1. **Objective** — A provisioned RED dashboard (dashboard-as-code, no click-ops).
2. **Concepts** — **Datasources, panels, provisioning, template variables,
   legends, units, histograms → heatmaps.**
3. **Components** — `grafana` container, `grafana/provisioning/{datasources,dashboards}/`,
   `grafana/dashboards/red.json`.
4. **Metrics** — existing.
5. **PromQL** — the Phase 4 queries, now as panels.
6. **Dashboards / panels**
   - **Rate**: RPS total, RPS by route
   - **Errors**: error % stat, 5xx by route
   - **Duration**: P50/P95/P99 timeseries, latency **heatmap** from `_bucket`
   - **Saturation**: in-flight gauge
   - **Availability**: `up` stat
   - `$route` template variable via `label_values(http_requests_total, route)`
7. **Repo changes** — `grafana/**`, `docker-compose.yml`, `docs/05-dashboards.md`.
8. **Definition of Done**
   - Fresh `docker compose up` auto-loads the datasource and dashboard
   - All panels populated under loadgen; heatmap shows `/slow`'s latency spread
   - `$route` filters the per-route panels; dashboard survives a full `down`/`up`
   - Tagged `phase-5`

---

## Phase 6 — Failure simulation

1. **Objective** — Break things on purpose; watch the dashboards react.
2. **Concepts** — **Correlating app behavior with signals**; what a latency
   problem vs an error problem vs a saturation problem looks like on a dashboard.
3. **Components** — `internal/api/chaos.go` (runtime-toggleable latency / forced
   5xx / `/cpu` burn / `/leak` allocator, behind an admin token),
   `loadgen/spike.js` (ramping-arrival-rate scenario).
4. **Metrics** — RED + `go_goroutines`, `go_memstats_heap_inuse_bytes`,
   `process_cpu_seconds_total` — now they matter.
5. **PromQL** — `rate(process_cpu_seconds_total[1m])`,
   `go_memstats_heap_inuse_bytes`, error-ratio query during a burst.
6. **Dashboards** — add a "Runtime / Saturation" row (CPU, heap, goroutines, GC pause).
7. **Repo changes** — `internal/api/chaos.go`, `internal/api/chaos_test.go`,
   `loadgen/spike.js`, `grafana/dashboards/red.json`, `docs/06-failure-playbook.md`.
8. **Definition of Done**
   - Toggling chaos raises error rate + P95 within one scrape
   - `/cpu` moves the CPU-rate panel; `/leak` grows heap; `/leak/reset` returns it
   - `spike.js` produces a visible traffic spike + matching in-flight/latency bump
   - The playbook maps every injected fault → the signal it produces
   - Tagged `phase-6`

---

## Phase 7 — Recording rules + alerting

1. **Objective** — Alerts fire and route to a receiver.
2. **Concepts** — **Recording rules** (precompute expensive queries), **alert
   rules**, `for:` duration, alert states (pending → firing), **Alertmanager**
   routing / grouping / inhibition. The chain:
   `rule evaluated in Prometheus → alert fires → pushed to Alertmanager →
   grouped/deduped → notification`.
3. **Components** — `alertmanager` container + `alertmanager/alertmanager.yml`,
   `prometheus/rules/recording.yml`, `prometheus/rules/alerts.yml`, a tiny
   `webhook-sink` service that logs received payloads.
4. **Metrics** — `ALERTS`, `ALERTS_FOR_STATE`, recording-rule outputs like
   `job:http_error_rate:ratio5m`.
5. **PromQL / rules**
   - `ApiDown`: `up{job="api"} == 0` for `1m`
   - `HighErrorRate`: `job:http_error_rate:ratio5m > 0.05` for `5m`
   - `HighLatencyP95`: `job:http_request_duration_seconds:p95_5m > 1` for `10m`
   - Inspect: `ALERTS{alertstate="firing"}`
6. **Dashboards** — `ALERTS`-based annotations + an "Active alerts" table panel.
7. **Repo changes** — `prometheus/rules/**`, `prometheus/prometheus.yml`
   (`rule_files`, `alerting`), `alertmanager/**`, `webhook-sink/**`,
   `docker-compose.yml`, `grafana/dashboards/red.json`, `docs/07-rules-alerting.md`.
8. **Definition of Done**
   - Sustained chaos errors → `HighErrorRate` goes pending → firing → webhook sink
     logs the payload
   - Stopping `app` fires `ApiDown`
   - A recording-rule-backed panel is visibly cheaper than the raw query
   - `promtool check rules` and `amtool check-config` pass (wired into `make check`)
   - Tagged `phase-7`

---

## Phase 8 — Infrastructure observability (deferred; modular sub-phases)

Gets its own `/sr:plan` before implementation. Sketch only:

| Sub-phase | Adds | Own metrics |
|-----------|------|-------------|
| 8a Postgres | `postgres` + `postgres_exporter`, real `internal/store` layer | `pg_stat_*`, `db_query_duration_seconds` |
| 8b Redis | `redis` + `redis_exporter` | `redis_commands_*`, `cache_hits_total` |
| 8c Kafka | `kafka` + `kafka_exporter` | `kafka_consumergroup_lag` |
| 8d Host/container | `node_exporter` + `cadvisor` | `node_cpu_seconds_total`, `container_memory_usage_bytes` |

**Concept** — exporters are the pattern for systems you can't instrument directly.

---

## Phase 9 — Structured logging

1. **Objective** — Add the logs pillar in pure application code, no new infra.
2. **Concepts** — The **canonical log line** (one wide JSON record per
   request); a **context-carried correlation ID** (`request_id`); a decorating
   `slog.Handler` that injects `trace_id`/`span_id` once Phase 11 exists;
   redaction by construction (the request logger never reads the body,
   headers, or query string) instead of a scrubber regex.
3. **Components** — `internal/obs` (context helpers, JSON handler,
   `PanicGuard`/`RequestLogger`/`TraceHTTP`-stub middleware), wired into
   `cmd/api`, `cmd/consumer`, `cmd/sink`, and the router's middleware chain.
4. **Metrics** — none new; RED metrics (Phase 2) are untouched.
5. **PromQL** — none.
6. **Dashboards** — none (Phase 12 adds a logs panel).
7. **Repo changes** — `internal/obs/**`, `internal/api/router.go` (new
   middleware order), `internal/api/chaos.go` + `handlers.go` (WARN on
   injected/simulated failure), `internal/cache`, `internal/events`
   (context-scoped logging), `internal/config` (`API_LOG_LEVEL`),
   `docs/09-structured-logging.md`.
8. **Definition of Done**
   - Every business/health/admin request emits exactly one `msg="request"`
     JSON line; `/metrics` emits none
   - A chaos-injected 500 logs a `WARN` sharing that request's `request_id`
   - `API_LOG_LEVEL=warn` suppresses INFO request lines
   - `go test ./...`, `go vet ./...`, `gofmt -l .` all clean
   - Tagged `phase-9`

---

## Phase 10 — Loki + Grafana Alloy

1. **Objective** — Ship Phase 9's JSON stdout logs somewhere queryable.
2. **Concepts** — **Push vs pull** (Alloy tails container stdout and pushes,
   the mirror of Prometheus's pull scrape); **Loki's label model** (a stream
   = a label set, same cardinality rule as a Prometheus series — stream
   labels stay `service`/`container`/`level`, everything else is queried
   from the JSON body with `| json`); **LogQL** as PromQL's sibling
   (`rate({...} | json | ...)` turns a log stream into a metric); retention
   needing both `retention_period` and `compactor.retention_enabled`.
3. **Components** — `loki` + `alloy` containers, `loki/loki-config.yml`,
   `alloy/config.alloy` (discovery → relabel → source.docker → process →
   write pipeline), a Loki Grafana datasource, a 3-panel "Logs" dashboard.
4. **Metrics** — none new for the app; Loki exposes its own
   `loki_ingester_memory_streams` (used for the cardinality demo).
5. **PromQL** — n/a (LogQL instead): `{service="app"} | json | level="ERROR"`,
   `sum by (route) (rate({service="app"} | json | __error__="" [5m]))`.
6. **Dashboards** — "Logs": 5xx-log-rate stat, request-rate-by-route
   timeseries derived from logs, an ERROR-level logs panel.
7. **Repo changes** — `loki/**`, `alloy/**`, `docker-compose.yml` (`loki`,
   `alloy`, `loki_data` volume), `grafana/provisioning/datasources/loki.yml`,
   `grafana/dashboards/logs.json`, `Makefile` (`check-config` extended,
   `logs` target), `docs/10-loki.md`.
8. **Definition of Done**
   - `make check-config` passes with Loki's config verified
   - Fresh `make up` → Alloy's `loki.source.docker` component green; logs
     queryable in Loki within ~15s of `make load`
   - `/loki/api/v1/labels` shows only `service`, `container`, `level`
     (+ Loki internals) — no `request_id`
   - "Logs" dashboard populated; the cardinality demo's real before/after
     `loki_ingester_memory_streams` numbers recorded
   - `make test` still green (no Go code touched)
   - Tagged `phase-10`

---

## Phase 11 — Distributed tracing

1. **Objective** — Wire a real end-to-end trace: root span in the router,
   child spans in the DB/cache/producer, `traceparent` carried across the
   Kafka boundary by hand, reconstructed in a separate consumer process.
2. **Concepts** — The **goroutine-span-lifetime trap** (a span started
   inside a detached `go func()` has no guaranteed live parent — start it
   synchronously before the goroutine, end it inside once the async work
   finishes); **W3C `traceparent` over a non-HTTP transport** (Kafka headers,
   hand-rolled since the OTel propagator only knows HTTP); **`ParentBased`
   sampling** (honor the root's sampling decision at every hop, never
   re-roll); **OTLP as a swappable wire format** (same property Prometheus's
   exposition format and Loki's push API have).
3. **Components** — `tempo` container (single-binary, OTLP receiver,
   metrics-generator remote-writing into Prometheus), root span via
   `obs.TraceHTTP`, child spans in `internal/store`/`internal/cache`,
   producer/consumer span pair across `internal/events`/`cmd/consumer`.
4. **Metrics** — none new for the app; Tempo's metrics-generator derives
   span-metrics + a service graph into Prometheus from trace data alone.
5. **PromQL/LogQL** — n/a (traces instead): unit-tested end-to-end via a
   `kafka.publish orders` span asserted as a child of the request root, its
   injected `traceparent` header round-tripping through `ParseTraceparent`.
6. **Dashboards** — Tempo Search in Grafana Explore; service graph from
   `serviceMap.datasourceUid: prometheus`.
7. **Repo changes** — `tempo/tempo-config.yml`, `docker-compose.yml`
   (`tempo`, OTLP env on `app`/`consumer`, Prometheus remote-write flag),
   `grafana/provisioning/datasources/tempo.yml`, `internal/obs/tracing.go` +
   `propagation.go` + the real `TraceHTTP` (Task 4), spans in
   `internal/store`, `internal/cache`, `internal/events/producer.go`,
   `cmd/consumer/main.go`, `loadgen/*.js` (traceparent origination),
   `docs/11-tracing.md`.
8. **Definition of Done**
   - `go test ./...` green, including the producer→header→consumer chain
   - `make check-config` includes Tempo, verified live (`-config.verify=true`)
   - `/health`, `/metrics` produce no spans
   - `API_OTLP_ENDPOINT=` (empty) → app/consumer still run, no export
   - `docs/11-tracing.md` covers the goroutine-span trap, sampling, the
     Jaeger swap, and why Tempo is pinned to `2.10.8` not `3.x`
   - Tagged `phase-11`

---

## The traps this lab teaches on purpose

| Trap | Where it bites | Fix taught |
|------|----------------|------------|
| `route` label = raw path (`/users/1`, `/users/2`, …) | P2 | Label by template; fall back to `"other"` |
| `rate(sum(...))` instead of `sum(rate(...))` | P4 | `rate()` needs the raw per-series counter to detect resets |
| Averaging quantiles across instances (Summary) | P2, P4 | Only histograms aggregate; use `histogram_quantile` on `_bucket` |
| Averaging an average latency | P4 | Percentiles from buckets, never a mean |
| Alert with no `for:` | P7 | Flappy; require a sustained window |
| Gauge used for a monotonic count | P2 | `rate()` breaks on a gauge; counters end in `_total` |
| Counters reset on restart | P3 | `rate`/`increase` handle resets; manual diffing doesn't |
| Rate window < ~4× scrape interval | P3, P4 | Gaps / NaN; standardize on `[5m]` |
| High-cardinality labels: `user_id`, `order_id`, `email`, `session_id`, raw path, timestamp, IP, unbounded error string | P2 | Label value sets must be small and bounded |
| Promoting a per-request field (`request_id`) to a Loki stream label | P10 | Stream count scales with traffic instead of staying flat; keep labels to `service`/`container`/`level` |
| Starting a span inside a detached `go func()` | P11 | No guaranteed live parent by the time it runs; start synchronously, end inside the goroutine |

## Metrics naming conventions

- `snake_case`; prefix by subsystem (`http_`, `db_`, `orders_`).
- Counters end in `_total`.
- Base units only: `_seconds` (not `_ms`), `_bytes` (not `_kb`). The suffix **is**
  the unit.
- The metric name says *what* is measured; **labels are the dimensions**. Never
  bake a dimension into the name: `http_requests_total{method="GET"}`, not
  `http_requests_get_total`.

## What differs in a real production system

- Service discovery (Kubernetes, Consul) instead of static targets; Pushgateway
  for short-lived jobs.
- Prometheus is not clustered — prod runs Thanos / Cortex / Mimir for HA,
  long-term storage, global query, and dedup; plus remote-write to a managed backend.
- TLS + auth + network policy on `/metrics` and every UI (the lab leaves them open).
- Real on-call: PagerDuty, escalation policies, silences, runbooks — not a webhook sink.
- Metrics correlated with traces (Tempo / Jaeger) and logs (Loki) — this lab
  builds that correlation too (Phases 9–12), just later and smaller-scale than
  a production deployment's log/trace retention and sampling budgets.
- Cardinality governance: series-count limits and alerts; one bad label can OOM
  Prometheus.
