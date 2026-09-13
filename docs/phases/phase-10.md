# Phase 10 — Loki + Grafana Alloy

**Status: DONE.** Verified live: fresh `make up`, Alloy's Docker pipeline
healthy, canonical request lines queryable in Loki within seconds of
`make load`, labels confirmed bounded (`service`/`container`/`level` only),
"Logs" dashboard datasource + panels confirmed against real Loki queries, and
the cardinality demo run for real (14 → 770 streams in 30s once `request_id`
was promoted to a label).

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
- [x] `make check-config` executed live against `loki -verify-config` —
      "config is valid"
- [x] Fresh `make up` → Alloy's Docker pipeline components all evaluated
      clean (`discovery.docker`, `loki.source.docker`, `loki.process`,
      `loki.write.default`)
- [x] `/loki/api/v1/labels` confirmed: `service`, `container`, `level` (+
      Loki's own `__stream_shard__`/`service_name` internals) — no
      `request_id`, `route`, or `trace_id`
- [x] Canonical `msg="request"` lines confirmed arriving in Loki via
      `{service="app"}` within seconds of `make load`
- [x] "Logs" dashboard's datasource + both LogQL panel queries confirmed
      returning real data straight from Loki
- [x] Cardinality demo run for real: 14 → 770 `loki_ingester_memory_streams`
      in 30s once `request_id` was promoted to a label; numbers in
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
