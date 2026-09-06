# Grafana RED Dashboard

Phase 5 adds Grafana to the Compose stack and provisions the Prometheus
datasource plus the RED dashboard from files. A fresh `make up` should load the
dashboard without clicking through the UI.

## Provisioning

Grafana provisioning turns local files into runtime configuration at startup.
`grafana/provisioning/datasources/prometheus.yml` creates the Prometheus
datasource, and `grafana/provisioning/dashboards/lab.yml` tells Grafana to load
dashboards from `grafana/dashboards`. The dashboard JSON in git is the source
of truth; UI edits are useful scratch work, but they need to be copied back to
disk or they disappear on a fresh container.

The datasource UID is pinned to `prometheus`. Dashboard panels refer to that UID
directly, so the dashboard reloads on another machine instead of pointing at an
instance-specific datasource ID. If a panel shows no data while the same query
works in Prometheus, the datasource UID is the first thing to check.

Dashboard queries use `$__rate_interval` instead of hardcoded `[5m]`. Grafana
expands it based on the scrape interval, time range, and panel width, which
keeps zoomed views from using a bad rate window. Route filtering uses the
`$route` variable from `label_values(http_requests_total, route)`. Because
multi-select and "All" expand to a regex, every route filter uses
`route=~"$route"`.

## Panels

**Total RPS** answers "how much traffic is the service handling?" Bad looks
like an unexpected drop to zero while `up` is still green, or an unexplained
traffic spike.

**RPS by route** answers "which endpoints are driving request volume?" Bad
looks like one route suddenly dominating traffic, or expected routes going
silent during load.

**Error %** answers "what fraction of selected traffic is failing with 5xx?"
Bad starts at the yellow threshold above 1% and is clearly red at 5% or more.
The value should rise quickly when `/error?rate=1` is exercised.

**5xx by route** answers "which route is producing server errors?" Bad looks
like any non-zero line outside an intentional simulator test; in this lab,
`/error` should be the usual source.

**Latency percentiles** answers "what do median, P95, and P99 latency look like
for the selected routes?" Bad is a widening gap between median and tail, or a
tail that climbs toward the highest bucket.

**Latency heatmap** answers "where is latency distributed over time?" Bad looks
like hot buckets moving upward or a second high-latency band becoming common.
Under normal load, `/slow` creates a visible band around the 0.5-2s buckets
while fast routes stay near zero.

**In-flight requests** answers "how many requests are active right now?" It is
a gauge, so there is no `rate()` in the query. Bad looks like sustained growth
or a value that stays high after load stops.

**API up** answers "can Prometheus scrape the API target?" It maps `1` to `UP`
and `0` to `DOWN`. Bad is `DOWN`, or flapping between states, which usually
means the target cannot be reached or the app is restarting.
