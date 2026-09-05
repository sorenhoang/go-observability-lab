# Metrics Reference

Phase 2 exposes a dedicated Prometheus registry at `/metrics`. The service does
not use the global default registry, so every test and process owns its own
collectors.

| Metric | Type | Labels | Question it answers |
| --- | --- | --- | --- |
| `http_requests_total` | Counter | `method`, `route`, `status` | How many requests finished by route and status? Error rate and request rate both come from `rate()` over this counter. |
| `http_request_duration_seconds` | Histogram | `method`, `route` | How long did requests take? Buckets let PromQL compute percentiles later, including across replicas. |
| `http_request_duration_summary_seconds` | Summary | `method`, `route` | Teaching contrast for the histogram. It computes quantiles in the client process at observation time. |
| `http_requests_in_progress` | Gauge | `route` | How many requests are currently being handled? This is saturation, so it must go up and down. |
| `orders_created_total` | Counter | none | How many orders were successfully created? This is the first business metric. |

The histogram is the latency metric to keep using after this phase. A Summary's
P95 is baked into each process before Prometheus scrapes it; averaging P95 from
two instances is not the real combined P95. Histograms export buckets, counts,
and sums, so Prometheus can aggregate buckets across instances and then compute
a fleet-level percentile.

The instrumentation wraps each route at registration time on one `http.ServeMux`.
Do not wrap an inner mux from the outside: `ServeMux` sets `r.Pattern` on the
request clone it dispatches to, so middleware outside a nested mux sees the
outer pattern instead of the real leaf route. Registering the middleware as the
leaf handler keeps the `route` label as the template, such as `/users`, not a
raw path like `/users?page=2`.

`/metrics` is deliberately not instrumented. Scrape traffic is not user traffic,
and counting it in `http_requests_total` would pollute the RED metrics. Unknown
paths are also not counted in this phase because `ServeMux` serves its own 404
before any wrapped route runs. A real service often adds a catch-all route for
that, using `route="other"`; this lab defers it so Phase 2 stays focused.
