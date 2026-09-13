# Phase 10 — Loki + Grafana Alloy

**Status: DONE** (config authored; live verification — Alloy health, log
arrival, the cardinality demo's real numbers — pending a `docker compose up`
run, since the Docker daemon wasn't available while this phase was authored).

## Objective

It's 2am. The RED dashboard shows `/orders`' error rate spiked ten minutes ago
and is still elevated. Which orders failed? Was it one bad product ID
repeated, or Postgres timing out, or chaos left on from an earlier test? The
metric says *something* is wrong; only the log line says *what*. This phase
ships Phase 9's JSON stdout logs to Loki so that question has an answer
without SSHing into a container.

## Concepts to internalize

- **Push vs pull, mirrored.** Prometheus pulls (`/metrics`, scraped). Loki
  is pushed to: Alloy tails container stdout via the Docker API and calls
  `loki.write`. The app is unaware of either — same "just expose/emit, let
  something else collect" shape.
- **Loki's label model is Prometheus's label model.** A stream = a unique
  label set, same cardinality discipline as a Prometheus series. Stream
  labels here: `service`, `container`, `level` — nothing else. `request_id`,
  `route`, `status` live inside the JSON line, queried with `| json`.
- **LogQL mirrors PromQL** — label matchers, pipeline stages, then an
  optional `rate()`/`sum by (...)` wrapper to turn a log stream into a numeric
  time series ("metrics from logs").
- **Retention needs both halves.** `retention_period` alone does nothing;
  `compactor.retention_enabled: true` (plus `delete_request_store` for
  filesystem storage) is what actually deletes old chunks.

## What to build

### 1. `loki/loki-config.yml`

Single-binary, filesystem storage, `tsdb` schema (the current recommended
index type), 72h retention with the compactor enabled to actually enforce it.

### 2. `alloy/config.alloy`

```
discovery.docker → discovery.relabel (container, service labels)
  → loki.source.docker → loki.process (stage.json extracts `level`,
    stage.label_keep bounds stream labels to service/container/level)
  → loki.write → http://loki:3100/loki/api/v1/push
```

### 3. `docker-compose.yml`

`loki` (port 3100, `loki_data` volume) and `alloy` (port 12345, read-only
Docker socket mount + `./alloy` config mount). `grafana` now depends on
`loki` too.

### 4. Grafana

`grafana/provisioning/datasources/loki.yml` (uid `loki`, `derivedFields: []`
— Phase 12 fills those in for the trace-ID click-through) and
`grafana/dashboards/logs.json`: three panels — a 5xx-log-rate stat, a
request-rate-by-route timeseries computed from logs, and an ERROR-level logs
panel.

### 5. `Makefile`

`check-config` now also runs `loki -verify-config` against
`loki/loki-config.yml`. New `logs` target opens Grafana Explore pointed at
the Loki datasource.

## Definition of Done

- [x] `loki/loki-config.yml`, `alloy/config.alloy` written; `docker compose
      config` validates the compose file; both YAML files parse
- [x] `grafana/provisioning/datasources/loki.yml` + `grafana/dashboards/logs.json`
      (3 panels) committed
- [x] `Makefile` `check-config` extended; new `logs` target
- [x] `docs/10-loki.md`, this file, README + roadmap updated
- [x] `make test` still green — no Go file touched
- [ ] **Pending a running Docker daemon:** `make check-config` actually
      executed against `loki -verify-config`; fresh `make up` → Alloy
      component green; `/loki/api/v1/labels` shows only `service`,
      `container`, `level` (+ internals); "Logs" dashboard populated under
      `make load`; the cardinality demo's real
      before/after `loki_ingester_memory_streams` numbers captured in
      `docs/10-loki.md`

## Traps to notice

- **Alloy needs the Docker socket, read-only is enough.** It only lists
  containers and reads their log streams — no write access needed, so mount
  `/var/run/docker.sock` `:ro`.
- **`stage.label_keep` is the cardinality guardrail, not `stage.labels`.**
  `stage.labels` promotes a parsed field to a label; without a following
  `stage.label_keep`, any other label Alloy picked up during discovery (Docker
  container labels, compose project name, …) rides along as a stream label
  too. Keep is the explicit allowlist.
- **`retention_enabled` without `delete_request_store` is a no-op.** Loki 3.x
  requires `delete_request_store` to be set explicitly when using filesystem
  object storage, or the compactor has nowhere to persist pending deletions
  and old chunks never actually get removed.
- **Pin `grafana/loki` and `grafana/alloy` tags.** Both projects ship
  frequently; an unpinned `latest` drifts out from under a docs walkthrough
  within weeks.
