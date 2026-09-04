# Glossary

Filled in as each phase teaches the term. Keep entries one or two lines — this is
a lookup, not a textbook.

## Metric types

- **Counter** — a value that only ever goes up (or resets to 0 on restart).
  Requests served, orders created, bytes sent. You never read a counter's raw
  value in a dashboard; you read its `rate()`. Name ends in `_total`. *(P2)*
- **Gauge** — a value that goes up and down. In-flight requests, queue depth,
  temperature, memory in use. Read directly. *(P2)*
- **Histogram** — samples observations into configurable buckets (`_bucket`),
  plus `_sum` and `_count`. Lets you compute *any* quantile later, in PromQL,
  aggregated across instances. Latencies and sizes. *(P2)*
- **Summary** — like a histogram but quantiles are computed client-side at
  observation time and baked into the exposition. Cannot be aggregated across
  instances. Used once in this lab purely as a contrast, then abandoned. *(P2)*

## Labels & cardinality

- **Label** — a key/value dimension on a metric: `http_requests_total{method="GET",
  route="/users", status="200"}`. Each unique combination is its own time series. *(P2)*
- **Cardinality** — the number of distinct time series a metric produces =
  product of its label value counts. High cardinality is the #1 way to blow up
  Prometheus. Never label with `user_id`, `order_id`, raw path, timestamp, email,
  session id, or IP. *(P2)*
- **Exposition format** — the plain-text format Prometheus scrapes:
  `metric_name{label="value"} 42.0` one per line, with `# HELP` / `# TYPE`
  comments. Visit `:8080/metrics` to read it raw. *(P2)*

## Collection

- **Scrape** — Prometheus fetching `/metrics` from a target over HTTP on an interval. *(P3)*
- **Target** — one thing Prometheus scrapes (a host:port + path + labels). *(P3)*
- **Job** — a named group of targets with the same purpose (`job="api"`). *(P3)*
- **`up`** — a metric Prometheus synthesizes per target: 1 if the last scrape
  succeeded, 0 if not. Your first availability signal. *(P3)*
- **`scrape_interval`** — how often Prometheus scrapes. Rate windows in PromQL
  must be ≥ ~4× this or you get gaps. *(P3)*

## PromQL

- **Instant vector** — one sample per matching series at a single instant. *(P4)*
- **Range vector** — a range of samples per series over a duration, e.g.
  `http_requests_total[5m]`. Required input to `rate()`. *(P4)*
- **`rate()`** — per-second average increase of a counter over a range,
  reset-aware. Always `sum(rate(x[5m]))`, never `rate(sum(x)[5m])`. *(P4)*
- **`histogram_quantile(φ, ...)`** — estimates the φ quantile from bucket rates. *(P4)*

## Rules & alerting

- **Recording rule** — a query Prometheus evaluates on a schedule and stores as a
  new series. Precomputes expensive/reused expressions. Named
  `level:metric:operation`. *(P7)*
- **Alert rule** — a query plus a `for:` duration; when it returns results for
  that long, an alert fires. *(P7)*
- **Alertmanager** — receives fired alerts from Prometheus, groups / dedupes /
  silences / inhibits them, and dispatches notifications. *(P7)*
- **`for:`** — how long an alert condition must hold before firing. Prevents
  flapping. Alert is `pending` during this window, then `firing`. *(P7)*

## Infra

- **Exporter** — a small process that reads metrics from a system you can't
  instrument directly (Postgres, Redis, the host) and exposes them in Prometheus
  format. *(P8)*
