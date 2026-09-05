# PromQL Cheatsheet

These are the Phase 4 RED queries for this lab. Counters are always derived
with `rate`, latency percentiles come from histogram buckets, and gauges are
read directly.

## 1. Total requests/sec

```promql
sum(rate(http_requests_total[5m]))
```

Answers: how much request traffic the whole service is handling per second.

Why: `http_requests_total` is a counter, so the raw value is only "since
process start." `rate` turns each counter series into per-second throughput,
then `sum` collapses method, route, status, job, and instance into one service
number.

Observed under load: about `5.67`.

## 2. Requests/sec by endpoint

```promql
sum by (route) (rate(http_requests_total[5m]))
```

Answers: which routes are receiving traffic, and how much.

Why: `rate` must happen before aggregation so counter resets are handled per
series. `sum by (route)` keeps only the route label and combines method,
status, job, and instance.

Observed under load: `/users` and `/products` dominated, with `/slow` and
`/error` lower.

## 3. Requests/sec by status

```promql
sum by (status) (rate(http_requests_total[5m]))
```

Answers: how much traffic is returning each HTTP status.

Why: this keeps `status` and drops the rest, so the result separates successes,
created orders, and simulated 500s without route noise.

Observed under load: `200`, `201`, and `500` series were present.

## 4. Error Ratio, Whole Service

```promql
sum(rate(http_requests_total{status=~"5.."}[5m]))
/
sum(rate(http_requests_total[5m]))
```

Answers: what fraction of requests are failing with 5xx responses.

Why: both numerator and denominator are rates over the same counter and the same
window. The regex selector limits the numerator to 5xx status codes. The result
is a ratio from `0` to `1`; multiply by 100 only at presentation time.

Observed under load: about `0.018`, or 1.8%.

## 5. Error Ratio Per Route

```promql
sum by (route) (rate(http_requests_total{status=~"5.."}[5m]))
/
sum by (route) (rate(http_requests_total[5m]))
```

Answers: which route is contributing errors.

Why: both sides keep the same `route` label, so Prometheus can divide matching
route series. In this lab only `/error` normally has 5xx responses.

Observed under load: `/error` had a non-zero error ratio; other routes had no
5xx numerator series.

## 6. P50 Latency

```promql
histogram_quantile(
  0.50,
  sum by (le) (rate(http_request_duration_seconds_bucket[5m]))
)
```

Answers: the median request latency across the service.

Why: histogram buckets are counters, so bucket series are first converted with
`rate`. `sum by (le)` aggregates all routes and methods while preserving `le`,
which `histogram_quantile` needs to estimate the percentile.

Observed under load: about `0.003s`.

## 7. P95 Latency

```promql
histogram_quantile(
  0.95,
  sum by (le) (rate(http_request_duration_seconds_bucket[5m]))
)
```

Answers: the latency below which about 95% of requests complete.

Why: this is the same bucket aggregation as P50, but asks for the tail. It is an
estimate bounded by bucket layout, not an exact raw sample percentile.

Observed under load: about `0.85s`.

## 8. P99 Latency

```promql
histogram_quantile(
  0.99,
  sum by (le) (rate(http_request_duration_seconds_bucket[5m]))
)
```

Answers: the high-tail latency for the service.

Why: keeping `le` lets Prometheus estimate a fleet-level percentile from
aggregated buckets. This is why the histogram is useful across instances.

Observed under load: about `1.78s`.

## 9. P95 Latency Per Route

```promql
histogram_quantile(
  0.95,
  sum by (le, route) (rate(http_request_duration_seconds_bucket[5m]))
)
```

Answers: which routes have the worst tail latency.

Why: `sum by (le, route)` keeps both the bucket boundary and route, so
`histogram_quantile` computes one P95 estimate per route.

Observed under load: `/slow` was the slow route, around `1.9s`.

## 10. Mean Latency, For Contrast

```promql
sum(rate(http_request_duration_seconds_sum[5m]))
/
sum(rate(http_request_duration_seconds_count[5m]))
```

Answers: average request latency.

Why: histogram `_sum` and `_count` are counters. Dividing the per-second sum of
observed seconds by the per-second count gives mean latency. This hides the tail
more than P95 does.

Observed under load: about `0.08s`, much lower than P95 because most requests
are fast.

## 11. In-Flight Requests Now

```promql
sum(http_requests_in_progress)
```

Answers: how many requests are currently being handled.

Why: this is a gauge. The current value is the answer, so there is no `rate`.
`sum` collapses routes into a service-wide saturation number.

Observed under load: often `0` between scrapes because most requests are short.

## 12. Peak In-Flight Over 5m

```promql
max_over_time(sum(http_requests_in_progress)[5m:])
```

Answers: the highest observed in-flight request count over the last 5 minutes.

Why: `max_over_time` needs a range vector. `sum(http_requests_in_progress)` is
an instant-vector expression, so the subquery `[5m:]` evaluates that expression
repeatedly over the last 5 minutes.

Observed under load: about `4`.

## 13. Availability Over 5m

```promql
avg_over_time(up{job="api"}[5m])
```

Answers: the fraction of successful scrapes for the API target in the last 5
minutes.

Why: `up` is a gauge-like scrape result, `1` for success and `0` for failure.
The time average is availability from `0` to `1`.

Observed under load: `1`.

## 14. Orders/sec

```promql
rate(orders_created_total[5m])
```

Answers: how many successful orders are created per second.

Why: `orders_created_total` is a business counter, so `rate` turns it into a
throughput signal. There are no labels to aggregate in this phase.

Observed under load: about `1.07`.

## 15. Is The Service Being Scraped?

```promql
up{job="api"}
```

Answers: whether Prometheus can currently scrape the API target.

Why: Prometheus creates `up` for every target. Filtering by job returns the API
target only.

Observed under load: `1`.

## Rules To Remember

- Use `sum(rate(counter[window]))`, not `rate(sum(counter)[window])`.
- Keep the rate window at least about four times the scrape interval.
- Preserve `le` when using `histogram_quantile`.
- Use `rate` for dashboards and alerts, `irate` only for high-resolution graph
  views, and `increase` for counts over a window.
- Read gauges directly or with `_over_time` functions; do not apply `rate` to a
  gauge.
