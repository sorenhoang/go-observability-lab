# PromQL Exercises

Try to answer these before looking below the divider.

1. Error rate ratio of `/orders` specifically, over the last 5 minutes.
2. Requests per second to `/users` and `/products` only, as one number.
3. How many orders were created in the last 10 minutes, as a count.
4. The route with the highest P95 latency right now.
5. Percentage of requests in the last 5 minutes that were not 2xx.
6. P90 latency of `POST` requests only.
7. Difference between mean and P50 latency for `/slow`, as one query showing the
   gap.
8. Number of distinct `(method, route, status)` combinations currently tracked.
9. Availability of the API over the last 1 minute, as a percentage.
10. Requests/sec using only the last 1 minute of data at high resolution.

---

## 1. `/orders` Error Ratio

```promql
(
  sum(rate(http_requests_total{route="/orders",status=~"5.."}[5m]))
  /
  sum(rate(http_requests_total{route="/orders"}[5m]))
)
or vector(0)
```

Verified result: `0`.

Why: the numerator selects only 5xx order requests and the denominator selects
all order requests. `or vector(0)` makes the current no-error case show as zero
instead of an empty vector.

## 2. Combined `/users` And `/products` RPS

```promql
sum(rate(http_requests_total{route=~"/users|/products"}[5m]))
```

Verified result: about `5.07`.

Why: the anchored regex matches either route, `rate` derives per-second counter
movement, and `sum` returns one combined number.

## 3. Orders Created In The Last 10 Minutes

```promql
increase(orders_created_total[10m])
```

Verified result: about `321`.

Why: this asks for a count over a time window, not per-second throughput, so
`increase` is the right counter function.

## 4. Route With Highest P95 Latency

```promql
topk(
  1,
  histogram_quantile(
    0.95,
    sum by (le, route) (rate(http_request_duration_seconds_bucket[5m]))
  )
)
```

Verified result: `/slow`, about `1.90s`.

Why: compute P95 per route first, preserving `le`, then let `topk` return the
worst one.

## 5. Percent Of Requests That Were Not 2xx

```promql
100 *
sum(rate(http_requests_total{status!~"2.."}[5m]))
/
sum(rate(http_requests_total[5m]))
```

Verified result: about `1.6%`.

Why: the negative regex excludes 2xx statuses. Multiplying by 100 makes the
ratio a percentage.

## 6. P90 Latency Of `POST` Requests

```promql
histogram_quantile(
  0.90,
  sum by (le) (rate(http_request_duration_seconds_bucket{method="POST"}[5m]))
)
```

Verified result: about `0.0045s`.

Why: filter the histogram buckets to POST requests before the `rate`, preserve
`le`, and compute the 90th percentile.

## 7. Mean Versus P50 Gap For `/slow`

```promql
abs(
  (
    sum(rate(http_request_duration_seconds_sum{route="/slow"}[5m]))
    /
    sum(rate(http_request_duration_seconds_count{route="/slow"}[5m]))
  )
  -
  histogram_quantile(
    0.50,
    sum by (le) (rate(http_request_duration_seconds_bucket{route="/slow"}[5m]))
  )
)
```

Verified result: about `0.074s`.

Why: the first half computes mean latency from histogram sum/count; the second
half computes estimated median from buckets. `abs` turns the subtraction into a
clear size-of-gap value even when bucket interpolation puts P50 slightly above
the mean.

## 8. Distinct `(method, route, status)` Combinations

```promql
count(count by (method, route, status) (http_requests_total))
```

Verified result: `6`.

Why: the inner `count by` creates one series per tracked label combination; the
outer `count` counts those series.

## 9. API Availability Over 1 Minute

```promql
avg_over_time(up{job="api"}[1m]) * 100
```

Verified result: `100`.

Why: `up` is `1` for a successful scrape and `0` for a failed scrape. Averaging
over time gives availability, then multiplying by 100 formats it as percent.

## 10. High-Resolution Requests/sec Over 1 Minute

```promql
sum(irate(http_requests_total[1m]))
```

Verified result: about `17.6`.

Why: `irate` uses the last two samples in the 1-minute window, so it reacts
quickly for a high-resolution graph. It is intentionally not the alerting or
dashboard default because one noisy scrape pair can spike it.
