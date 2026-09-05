# Phase 5 — Grafana RED dashboard

**Status: TODO — you build this manually, then ask for a review.**

## Objective

A **provisioned** RED dashboard — datasource and dashboard both defined as
code, so a fresh `make up` brings up Grafana with the dashboard already there,
no clicking. Every panel query is one you already understand from Phase 4.

## Concepts to internalize

- **Provisioning** — Grafana reads YAML files at startup to create datasources
  and load dashboards from disk. The alternative (clicking around the UI and
  hoping you remember what you did) isn't reproducible. Dashboards live in git
  as JSON.
- **Datasource UID** — a dashboard JSON references its datasource by UID, not
  name. If you let Grafana auto-generate the UID, the exported JSON won't match
  on someone else's machine. **Pin the UID in the provisioning YAML**
  (`uid: prometheus`) and reference that exact string in every panel.
- **`$__rate_interval`** — in Phase 4 you hardcoded `[5m]`. On a dashboard, use
  Grafana's `$__rate_interval` macro instead: it expands to at least 4× the
  scrape interval *and* adapts to the panel's time range and width, so a panel
  zoomed to 24h doesn't try to draw 17,000 points.
- **Template variables** — `$route` is a dropdown populated by
  `label_values(http_requests_total, route)`. Panels filter with
  `{route=~"$route"}`. With "Multi-value" + "Include All" on, `$route` expands
  to a regex alternation, which is why the matcher is `=~` not `=`.
- **Heatmap from a histogram** — Grafana's heatmap panel takes the raw
  `_bucket` series (format: **Heatmap**, legend `{{le}}`) and renders latency
  distribution over time. It's the one view where you *see* the bimodal shape
  of `/slow` vs everything else.

## What to build

### 1. Add `grafana` to `docker-compose.yml`

```yaml
  grafana:
    image: grafana/grafana:12.3.0
    ports:
      - "${GRAFANA_HOST_PORT:-3000}:3000"
    environment:
      GF_AUTH_ANONYMOUS_ENABLED: "true"
      GF_AUTH_ANONYMOUS_ORG_ROLE: "Admin"
      GF_AUTH_DISABLE_LOGIN_FORM: "true"
    volumes:
      - ./grafana/provisioning:/etc/grafana/provisioning:ro
      - ./grafana/dashboards:/var/lib/grafana/dashboards:ro
      - grafana_data:/var/lib/grafana
    depends_on:
      - prometheus
```

Anonymous admin is fine for a local lab — no login wall between you and the
dashboard. Add `grafana_data` to the `volumes:` block at the bottom of the file.

### 2. `grafana/provisioning/datasources/prometheus.yml`

```yaml
apiVersion: 1

datasources:
  - name: Prometheus
    type: prometheus
    uid: prometheus          # pinned — panels reference this exact string
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    jsonData:
      timeInterval: 5s       # tells Grafana the scrape interval for $__rate_interval
```

### 3. `grafana/provisioning/dashboards/lab.yml`

```yaml
apiVersion: 1

providers:
  - name: lab
    type: file
    allowUiUpdates: true     # you WILL iterate in the UI; this lets you save
    updateIntervalSeconds: 10
    options:
      path: /var/lib/grafana/dashboards
      foldersFromFilesStructure: false
```

`allowUiUpdates: true` matters: without it, "Save" in the UI is greyed out and
you can't iterate. With it, you build in the UI, then **copy the saved JSON
back into `grafana/dashboards/red.json`** and commit — the file on disk stays
the source of truth.

### 4. `grafana/dashboards/red.json` — the dashboard

**Recommended workflow:** don't hand-write 300 lines of JSON. Instead:

1. Create an empty `grafana/dashboards/red.json` containing `{}` so provisioning
   has something to load, `make up`, open Grafana.
2. Build the panels in the UI (spec below).
3. Dashboard settings → **JSON Model** → copy it into `red.json`.
4. Delete the top-level `"id"` field (it's instance-specific); keep `"uid"`.
5. `make down && make up` and confirm it reloads from disk identically.

**Panel spec** — every query uses datasource uid `prometheus` and
`$__rate_interval`:

| Row | Panel | Type | Query | Notes |
|-----|-------|------|-------|-------|
| **Rate** | Total RPS | Time series | `sum(rate(http_requests_total{route=~"$route"}[$__rate_interval]))` | unit: `reqps` |
| | RPS by route | Time series | `sum by (route) (rate(http_requests_total{route=~"$route"}[$__rate_interval]))` | legend `{{route}}` |
| **Errors** | Error % | Stat | `100 * sum(rate(http_requests_total{status=~"5..",route=~"$route"}[$__rate_interval])) / sum(rate(http_requests_total{route=~"$route"}[$__rate_interval]))` | unit `percent`, thresholds green/yellow/red at 0/1/5 |
| | 5xx by route | Time series | `sum by (route) (rate(http_requests_total{status=~"5..",route=~"$route"}[$__rate_interval]))` | |
| **Duration** | Latency percentiles | Time series | 3 queries: `histogram_quantile(0.50\|0.95\|0.99, sum by (le) (rate(http_request_duration_seconds_bucket{route=~"$route"}[$__rate_interval])))` | unit `s`, legend `p50/p95/p99` |
| | Latency heatmap | Heatmap | `sum(rate(http_request_duration_seconds_bucket{route=~"$route"}[$__rate_interval])) by (le)` | format **Heatmap**, legend `{{le}}` |
| **Saturation** | In-flight requests | Time series | `sum(http_requests_in_progress{route=~"$route"})` | not a rate — it's a gauge |
| **Availability** | API up | Stat | `up{job="api"}` | value mappings: 1→UP (green), 0→DOWN (red) |

**Template variable** — Dashboard settings → Variables → New:
- Name: `route`, Type: Query, Datasource: Prometheus
- Query: `label_values(http_requests_total, route)`
- Multi-value: on, Include All option: on, Custom all value: `.*`

Set the dashboard refresh to `5s` and default time range to `Last 15 minutes`.

### 5. Makefile

`make up` already starts everything via `docker compose up`. Add a convenience
target if you want:

```makefile
dash: ## Open the Grafana dashboard
	open http://localhost:3000/d/red || xdg-open http://localhost:3000/d/red
```

### 6. `docs/05-dashboards.md`

Cover: what provisioning is and why (reproducibility), the datasource-UID
pinning gotcha, `$__rate_interval` vs a hardcoded window, how `$route` becomes
a regex, and one paragraph per panel — *what question it answers and what
"bad" looks like on it*.

---

## Definition of Done

```sh
make up
make load
make dash    # or open http://localhost:3000
```

- [ ] Fresh `make up` (after `docker compose down -v`) loads the datasource
      **and** the dashboard with zero clicks
- [ ] All 8 panels show data within ~30s of `make load`
- [ ] The latency heatmap visibly shows `/slow`'s spread (a band up around
      0.5–2s) separate from the fast routes (a band near 0)
- [ ] `$route` dropdown lists every route; picking `/slow` filters every
      per-route panel
- [ ] Error % stat goes yellow/red when you run `curl "localhost:8080/error?rate=1"` in a loop
- [ ] `docker compose down && make up` — dashboard comes back identical from disk
- [ ] `red.json` has no top-level `"id"`, keeps `"uid"`, references datasource
      uid `prometheus` everywhere
- [ ] `docs/05-dashboards.md` written
- [ ] Committed, tagged `phase-5`

## Traps to notice

- **Panel shows "No data" but the query works in Prometheus** — almost always
  a datasource UID mismatch. Check the panel's datasource is uid `prometheus`,
  not a dangling reference from an export.
- **Heatmap looks like a mess of lines** — you left the query format as "Time
  series" instead of "Heatmap", or you didn't `sum by (le)`.
- **`$route` shows `{route="/slow"}` literally in results** — you used `=`
  instead of `=~`, so the regex-alternation value from "Include All" doesn't
  match.
- **Dashboard resets every `make up`** — you're editing in the UI but not
  copying JSON back to `red.json`. The file is the source of truth; the UI is
  scratch space.
- **Provisioned dashboard won't save from UI** — `allowUiUpdates` isn't set.
