# Phase 4 — PromQL practice

**Status: DONE.** `docs/promql-cheatsheet.md` (15 RED queries, each explained)
and `docs/04-promql-exercises.md` (10 exercises, all verified live) written.

## Objective

Be able to write the RED queries **unaided** and explain *why* each is shaped
the way it is. Every query here becomes a Grafana panel in Phase 5 — if you
paste queries you don't understand, you can't debug a panel that looks wrong.

## Setup

```sh
make up
make load     # let it run the whole session — you need moving data
open http://localhost:9090/graph
```

Do everything in the Prometheus **expression browser** (the `/graph` page).
Toggle between "Table" (instant result) and "Graph" (range) views — that
distinction is the first concept below.

## Deliverables

1. `docs/promql-cheatsheet.md` — every query below, plus **the question it
   answers** and **one line on why it's written that way**, in your own words.
2. `docs/04-promql-exercises.md` — the exercises at the bottom, with your
   answers below a `---` divider.

---

## Concept 1 — What you're querying

Every **series** is a metric name + a set of labels:
`http_requests_total{method="GET", route="/users", status="200"}`. Prometheus
stores a stream of `(timestamp, float64)` samples for each series.

Three result types:

| Type | What | Example | Where it shows |
|------|------|---------|----------------|
| **Instant vector** | one sample per series, at one instant | `http_requests_total` | Table view |
| **Range vector** | a *window* of samples per series | `http_requests_total[5m]` | only valid as input to a function |
| **Scalar** | a single number | `1`, `0.95` | — |

A range vector on its own (`http_requests_total[5m]`) is not something you
graph — it's raw material for `rate()`, `increase()`, `avg_over_time()`, etc.

## Concept 2 — Selectors

```promql
http_requests_total{route="/users"}          # exact match
http_requests_total{status=~"5.."}           # regex match (5xx)
http_requests_total{status!~"2..|3.."}       # regex negative (not 2xx/3xx)
http_requests_total{route!="/metrics"}       # not equal
```

`=~` / `!~` are full-string anchored regex. `5..` means "5 then any two chars".

## Concept 3 — `rate`, `irate`, `increase` (counters only)

A counter's raw value is meaningless (it's just "total since this process
started"). You always derive from it:

```promql
rate(http_requests_total[5m])       # per-second avg increase over 5m, reset-aware
irate(http_requests_total[5m])      # per-second rate from the LAST TWO samples in the window
increase(http_requests_total[5m])   # total increase over 5m  (== rate * 300)
```

- **`rate`** — smoothed, what you want for dashboards and alerts. Reset-aware:
  if the process restarts and the counter drops to 0, `rate` handles it.
- **`irate`** — only looks at the last two samples. Very responsive, very
  spiky. Use for graphing fast-moving signals at high resolution; **never for
  alerting** (one unlucky pair of samples fires it).
- **`increase`** — "how many happened in the last hour". Same data as `rate`,
  scaled to the window instead of per-second.

**The window must be ≥ ~4× the scrape interval.** Scrape is `5s`, so `[5m]`
gives ~60 samples — plenty. `[10s]` would give 2 and be garbage.

## Concept 4 — Aggregation, and the `sum(rate(...))` rule

`rate()` gives you one series *per label combination*. To get a single number
you aggregate:

```promql
sum(rate(http_requests_total[5m]))                      # total RPS
sum by (route)   (rate(http_requests_total[5m]))        # RPS per route
sum by (status)  (rate(http_requests_total[5m]))        # RPS per status
```

`sum by (route)` keeps `route`, collapses everything else. `sum without
(status)` is the inverse — keep everything *except* `status`.

**`sum(rate(x[5m]))` — never `rate(sum(x)[5m])`.** `rate()` needs each
individual counter series to detect resets (a restart drops one instance's
counter to 0). `sum()` first would blend resets into the total and produce
negative spikes. Also: `rate(sum(...)[5m])` isn't even valid — `sum(...)` is an
instant vector, you can't put `[5m]` on it.

Other aggregators: `avg`, `min`, `max`, `count`, `topk(3, ...)`,
`bottomk(3, ...)`.

## Concept 5 — Error ratio (vector matching + division)

```promql
sum(rate(http_requests_total{status=~"5.."}[5m]))
/
sum(rate(http_requests_total[5m]))
```

Both sides are scalars-shaped instant vectors with no labels, so they divide
directly. Result is `0.0`–`1.0` (multiply by 100 for a percentage panel).

**Gotcha:** when there's zero traffic the denominator is 0 → result is `NaN`,
which shows as a gap. That's usually fine; if you need `0` instead, wrap it:
`(... ) or vector(0)`.

Per-route error ratio needs matching labels on both sides:

```promql
sum by (route) (rate(http_requests_total{status=~"5.."}[5m]))
/
sum by (route) (rate(http_requests_total[5m]))
```

## Concept 6 — Latency percentiles from a histogram

The histogram exposes `http_request_duration_seconds_bucket{le="..."}` —
cumulative counts per upper bound. To get P95:

```promql
histogram_quantile(
  0.95,
  sum by (le) (rate(http_request_duration_seconds_bucket[5m]))
)
```

Read it inside-out:
1. `rate(..._bucket[5m])` — per-second rate for **every** bucket series.
2. `sum by (le) (...)` — add up bucket rates across `method`/`route`, keeping
   only `le`. **`le` must survive the `by` clause** or `histogram_quantile`
   has nothing to work with.
3. `histogram_quantile(0.95, ...)` — interpolates within the bucket that
   contains the 95th percentile.

Per route: `sum by (le, route)` instead of `sum by (le)`.

**Caveats:**
- The result is an **estimate**, bounded by your bucket boundaries. Your top
  finite bucket is `3` (seconds); a real P99 of 2.5s is estimated by
  interpolating inside the `1`–`2` and `2`–`3` buckets. A P99 above `3` reads
  as `+Inf` / the last boundary.
- **Never average a histogram's derived percentile the way you'd average a
  gauge.** The whole point of the bucket approach is that step 2 (`sum by
  (le)`) aggregates the *raw buckets* across instances first, *then* computes
  one honest fleet-wide percentile. Averaging per-instance P95s (what a Summary
  forces you to do) is not a real P95.
- P50 from the histogram ≈ median. Compare it to
  `rate(..._sum[5m]) / rate(..._count[5m])` (the mean) and watch them diverge
  when `/slow` is active — mean is dragged by the tail, median isn't.

## Concept 7 — Over-time functions (aggregate a range, per series)

`rate` and friends turn a counter range into a rate. `_over_time` functions
aggregate *any* range vector along the time axis, per series:

```promql
avg_over_time(http_requests_in_progress[5m])   # avg in-flight over 5m
max_over_time(http_requests_in_progress[5m])   # peak in-flight over 5m
avg_over_time(up{job="api"}[5m])               # availability: fraction of successful scrapes
```

`avg_over_time(up[5m])` = `0.0`–`1.0`. `1` means every scrape in the window
succeeded; `0.9` means ~10% failed. This is your SLA-style availability number.

## Concept 8 — In-flight (gauge, read directly)

```promql
sum(http_requests_in_progress)                 # total in-flight now
http_requests_in_progress                       # per route
```

No `rate()` — it's a gauge, its current value *is* the answer.

---

## The RED query set (put these in the cheatsheet)

| # | Question | Query |
|---|----------|-------|
| 1 | Total requests/sec | `sum(rate(http_requests_total[5m]))` |
| 2 | Requests/sec by endpoint | `sum by (route) (rate(http_requests_total[5m]))` |
| 3 | Requests/sec by status | `sum by (status) (rate(http_requests_total[5m]))` |
| 4 | Error ratio (whole service) | `sum(rate(http_requests_total{status=~"5.."}[5m])) / sum(rate(http_requests_total[5m]))` |
| 5 | Error ratio per route | `sum by (route) (rate(http_requests_total{status=~"5.."}[5m])) / sum by (route) (rate(http_requests_total[5m]))` |
| 6 | P50 latency | `histogram_quantile(0.50, sum by (le) (rate(http_request_duration_seconds_bucket[5m])))` |
| 7 | P95 latency | `histogram_quantile(0.95, sum by (le) (rate(http_request_duration_seconds_bucket[5m])))` |
| 8 | P99 latency | `histogram_quantile(0.99, sum by (le) (rate(http_request_duration_seconds_bucket[5m])))` |
| 9 | P95 latency per route | `histogram_quantile(0.95, sum by (le, route) (rate(http_request_duration_seconds_bucket[5m])))` |
| 10 | Mean latency (for contrast) | `sum(rate(http_request_duration_seconds_sum[5m])) / sum(rate(http_request_duration_seconds_count[5m]))` |
| 11 | In-flight requests now | `sum(http_requests_in_progress)` |
| 12 | Peak in-flight over 5m | `max_over_time(sum(http_requests_in_progress)[5m:])` |
| 13 | Availability over 5m | `avg_over_time(up{job="api"}[5m])` |
| 14 | Orders/sec | `rate(orders_created_total[5m])` |
| 15 | Is the service being scraped? | `up{job="api"}` |

> #12 uses a **subquery** `[5m:]` — `sum(http_requests_in_progress)` is an
> instant vector, and `max_over_time` needs a range, so `[5m:]` tells
> Prometheus "evaluate this expression repeatedly over the last 5m". Note the
> trailing colon. Don't overuse subqueries — they're expensive.

---

## Exercises (write these in `docs/04-promql-exercises.md`)

Answer each with a query, run it, and confirm it returns sensible data. Put
answers below a `---` divider so you can re-test yourself later.

1. Error rate (ratio) of `/orders` specifically, over the last 5 minutes.
2. Requests per second to `/users` **and** `/products` only, as one number.
3. How many orders were created in the last 10 minutes (a count, not a rate)?
4. The route with the highest P95 latency right now (one series, the worst one).
5. Percentage of requests in the last 5m that were **not** 2xx.
6. P90 latency of `POST` requests only.
7. Difference between mean and P50 latency for `/slow` — one query showing the gap.
8. Number of distinct `(method, route, status)` combinations currently being tracked.
9. Availability of the API over the last 1 minute, as a percentage.
10. Requests/sec, but only counting the last 1m of data at high resolution
    (which function, and why not `rate`?).

---

## Common mistakes this phase should burn in

- `rate(sum(...))` — wrong order, and invalid syntax. Always `sum(rate(...))`.
- Rate window shorter than ~4 scrape intervals → jagged / empty graphs.
- Forgetting `le` in the `by ()` clause of `histogram_quantile` → `NaN`.
- Reading a raw counter value off a dashboard ("we've served 4,102,338
  requests" — since when? which replica? meaningless).
- Averaging percentiles across instances instead of aggregating buckets first.
- Using `irate` for an alert → flaps on a single noisy sample pair.
- Charting a gauge with `rate()` → nonsense (it's not monotonic).

## Definition of Done

- [ ] `docs/promql-cheatsheet.md` — all 15 queries, each with your own
      "question it answers" + "why written this way"
- [ ] `docs/04-promql-exercises.md` — all 10 exercises answered and verified
      against live data
- [ ] You can write query #4 (whole-service error ratio) and #7 (P95) from
      memory and explain every nested layer
- [ ] Committed, tagged `phase-4`
