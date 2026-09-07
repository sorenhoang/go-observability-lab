# Phase 7 Recording Rules and Alerting

Phase 7 turns the PromQL from the dashboard into named recorded series, then
builds alerts on those series. The chain is:

```text
raw metrics -> recording rules -> alert rules -> Alertmanager -> webhook sink
```

Prometheus owns the first three steps. It scrapes raw metrics, evaluates rules,
and decides whether an alert is inactive, pending, or firing. Alertmanager owns
notification delivery: grouping, deduplication, repeat timing, silences,
inhibition, and receiver dispatch.

## Recording Rules

The recording rules live in `prometheus/rules/recording.yml` and use the
`level:metric:operation` naming convention:

| Recorded series | Meaning |
| --- | --- |
| `job:http_requests:rate5m` | Total API request rate over five minutes. |
| `job:http_error_rate:ratio5m` | 5xx requests divided by all requests over five minutes. |
| `job:http_request_duration_seconds:p95_5m` | Aggregate P95 latency from histogram buckets over five minutes. |

Alerts and dashboard panels should use these short names when they need the same
expensive expression repeatedly. Prometheus evaluates the heavy query once per
rule interval and stores the result as a normal time series.

## Alert Lifecycle

An alert expression that returns a series becomes `pending` immediately. It only
becomes `firing` after the expression remains true for the whole `for:` window.
That delay is intentional: it filters out one bad scrape, a brief deploy blip,
or a noisy test request.

The Phase 7 alerts are:

| Alert | Expression | `for:` | Severity |
| --- | --- | --- | --- |
| `ApiDown` | `up{job="api"} == 0` | 1m | critical |
| `HighErrorRate` | `job:http_error_rate:ratio5m > 0.05` | 5m | warning |
| `HighLatencyP95` | `job:http_request_duration_seconds:p95_5m > 1` | 10m | warning |

The error-rate and latency alerts use the recorded series, not the raw nested
PromQL. That keeps the alert readable and proves the recording rule is pulling
its weight.

The dashboard's **Error %** stat is also repointed to `job:http_error_rate:ratio5m`.
A side effect: that panel is now whole-service and ignores the `$route`
variable — the recorded series has no `route` dimension. That is intentional
here: the stat now shows exactly the number `HighErrorRate` alerts on, so the
dashboard and the alert can never disagree. The per-route view is still in the
**5xx by route** panel next to it.

## Alertmanager

Prometheus sends firing and resolved alerts to Alertmanager at
`alertmanager:9093`. Alertmanager groups by `alertname`, waits `10s` for related
alerts before sending, repeats still-firing notifications every `5m`, and posts
to the local webhook sink at `http://webhook-sink:9000/webhook`.

The sink logs one line per alert with status, name, severity, and summary. Use
it to prove the notification left Prometheus, passed through Alertmanager, and
reached a receiver.

## Walkthrough: High Error Rate

Start the stack and load generator:

```sh
make up
make load
```

Confirm the recorded series exists:

```sh
curl -s 'localhost:9090/api/v1/query?query=job:http_error_rate:ratio5m' | jq '.data.result'
```

Trigger sustained errors:

```sh
curl -s -XPOST localhost:8080/admin/chaos -d '{"enabled":true,"error_ratio":0.3}'
```

Watch the alert lifecycle:

```sh
watch -n5 'curl -s "localhost:9090/api/v1/query?query=ALERTS" | jq -c ".data.result[] | {name: .metric.alertname, state: .metric.alertstate}"'
```

`HighErrorRate` should appear as `pending` first, then `firing` after the full
five-minute `for:` window. Alertmanager receives the firing alert and the sink
logs it after the `group_wait` delay:

```sh
docker compose logs -f webhook-sink
```

Resolve it by disabling chaos:

```sh
curl -s -XPOST localhost:8080/admin/chaos -d '{"enabled":false}'
```

Because the webhook receiver has `send_resolved: true`, the sink should log the
resolved notification too.
