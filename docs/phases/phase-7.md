# Phase 7 — Recording rules + alerting

**Status: DONE.** Recording rules + 3 alerts, Alertmanager routing to a local cmd/sink webhook. Full chain verified live: stop app -> ApiDown pending -> firing at ~88s -> Alertmanager -> sink logs `status=firing`; restart -> `status=resolved` (send_resolved).

## Objective

Precompute expensive queries with **recording rules**, define **alert rules**
that fire on sustained bad conditions, route them through **Alertmanager**, and
watch a notification actually land in a local webhook sink. By the end you can
trace one alert from `rule evaluated → pending → firing → grouped → delivered`.

## Concepts to internalize

- **Recording rule** — a query Prometheus evaluates on a schedule and stores as
  a brand-new time series. Two reasons: (1) a dashboard panel or alert that
  reuses a heavy expression evaluates it once, not every refresh; (2) the
  stored series has a stable, short name. Named `level:metric:operation`
  (`job:http_error_rate:ratio5m`).
- **Alert rule** — a query + a `for:` duration + labels + annotations. When the
  query returns any series continuously for `for:`, the alert goes `firing`.
- **`for:` and alert states** — the instant the query matches, the alert is
  `pending`. It only becomes `firing` after the condition holds for the whole
  `for:` window. Without `for:`, a single bad scrape pages you at 3am.
- **Alertmanager** — Prometheus doesn't send notifications; it pushes firing
  alerts to Alertmanager, which **groups** related alerts, **deduplicates**
  across replicas, **silences**, **inhibits** (suppress X while Y is firing),
  and dispatches to receivers.
- **The split:** Prometheus decides *what is wrong*. Alertmanager decides *who
  hears about it and how often*.

## What to build

### 1. `prometheus/rules/recording.yml`

```yaml
groups:
  - name: http_red
    interval: 15s
    rules:
      - record: job:http_requests:rate5m
        expr: sum(rate(http_requests_total[5m]))

      - record: job:http_error_rate:ratio5m
        expr: |
          sum(rate(http_requests_total{status=~"5.."}[5m]))
          /
          sum(rate(http_requests_total[5m]))

      - record: job:http_request_duration_seconds:p95_5m
        expr: |
          histogram_quantile(
            0.95,
            sum by (le) (rate(http_request_duration_seconds_bucket[5m]))
          )
```

### 2. `prometheus/rules/alerts.yml`

```yaml
groups:
  - name: api_alerts
    rules:
      - alert: ApiDown
        expr: up{job="api"} == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "API target is down"
          description: "Prometheus cannot scrape {{ $labels.instance }} for over 1m."

      - alert: HighErrorRate
        expr: job:http_error_rate:ratio5m > 0.05
        for: 5m
        labels:
          severity: warning
        annotations:
          summary: "5xx error ratio above 5%"
          description: "Error ratio is {{ $value | humanizePercentage }} (threshold 5%) for 5m."

      - alert: HighLatencyP95
        expr: job:http_request_duration_seconds:p95_5m > 1
        for: 10m
        labels:
          severity: warning
        annotations:
          summary: "P95 latency above 1s"
          description: "P95 is {{ $value | humanizeDuration }} (threshold 1s) for 10m."
```

Note the alert exprs use the **recorded** series, not the raw histogram — that
is the recording rule earning its keep.

### 3. `prometheus/prometheus.yml` — wire rules + Alertmanager

```yaml
global:
  scrape_interval: 5s

rule_files:
  - /etc/prometheus/rules/*.yml

alerting:
  alertmanagers:
    - static_configs:
        - targets:
            - alertmanager:9093

scrape_configs:
  - job_name: api
    static_configs:
      - targets: ["app:8080"]

  - job_name: alertmanager
    static_configs:
      - targets: ["alertmanager:9093"]
```

(Scraping Alertmanager itself is optional but cheap — you get `alertmanager_*`
metrics and can alert on your alerting being down.)

### 4. `alertmanager/alertmanager.yml`

```yaml
route:
  receiver: webhook
  group_by: ['alertname']
  group_wait: 10s
  group_interval: 30s
  repeat_interval: 5m

receivers:
  - name: webhook
    webhook_configs:
      - url: http://webhook-sink:9000/webhook
        send_resolved: true
```

`group_wait: 10s` — after the first alert in a group fires, wait 10s for
siblings before sending one bundled notification. `repeat_interval: 5m` — if
still firing, re-notify every 5m (short, for a lab; production is hours).

### 5. `cmd/sink/main.go` — the webhook receiver

A tiny HTTP server that decodes the Alertmanager webhook payload and logs one
line per alert. This is boilerplate — the point is *seeing* the delivery, not
building a notifier.

```go
package main

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"time"
)

type amPayload struct {
	Status string `json:"status"`
	Alerts []struct {
		Status      string            `json:"status"`
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations"`
	} `json:"alerts"`
}

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))

	mux := http.NewServeMux()
	mux.HandleFunc("POST /webhook", func(w http.ResponseWriter, r *http.Request) {
		var p amPayload
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, "bad payload", http.StatusBadRequest)
			return
		}
		for _, a := range p.Alerts {
			slog.Info("alert",
				"status", a.Status,
				"name", a.Labels["alertname"],
				"severity", a.Labels["severity"],
				"summary", a.Annotations["summary"],
			)
		}
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{Addr: ":9000", Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	slog.Info("webhook sink listening", "addr", ":9000")
	if err := srv.ListenAndServe(); err != nil {
		slog.Error("sink stopped", "err", err)
		os.Exit(1)
	}
}
```

### 6. Dockerfile — build both binaries

Change the build + copy so the image carries `/api` and `/sink`:

```dockerfile
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/ ./cmd/...
...
COPY --from=build /out/ /
ENTRYPOINT ["/api"]
```

### 7. `docker-compose.yml` — add alertmanager + webhook-sink

```yaml
  alertmanager:
    image: prom/alertmanager:v0.28.1
    ports:
      - "${ALERTMANAGER_HOST_PORT:-9093}:9093"
    volumes:
      - ./alertmanager/alertmanager.yml:/etc/alertmanager/alertmanager.yml:ro
    depends_on:
      - webhook-sink

  webhook-sink:
    build: .
    entrypoint: ["/sink"]
```

And mount the rules dir into prometheus:

```yaml
    volumes:
      - ./prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - ./prometheus/rules:/etc/prometheus/rules:ro
      - prometheus_data:/prometheus
```

### 8. `make` targets

```makefile
check-config: ## Validate prometheus rules + alertmanager config
	docker run --rm -v $(PWD)/prometheus:/p prom/prometheus:v3.7.3 \
	  promtool check rules /p/rules/recording.yml /p/rules/alerts.yml
	docker run --rm -v $(PWD)/alertmanager:/a prom/alertmanager:v0.28.1 \
	  amtool check-config /a/alertmanager.yml
```

### 9. Dashboard additions to `red.json`

- Repoint the **Error %** stat's query to `job:http_error_rate:ratio5m * 100`
  (was the full nested expression) — note the panel still renders, cheaper.
- Add an **"Active alerts"** table panel: `ALERTS{alertstate="firing"}`, table
  format, showing `alertname`, `severity`.
- Add an **annotation**: `ALERTS{alertstate="firing"}`, so firing alerts mark
  every timeseries panel.

### 10. `docs/07-rules-alerting.md`

Cover: recording rule naming, the `pending → firing` lifecycle and why `for:`
exists, the Prometheus/Alertmanager split, grouping/`repeat_interval`, and a
walkthrough of triggering `HighErrorRate` and watching it flow to the sink.

---

## Definition of Done

```sh
make check-config          # rules + AM config valid
make up && make load
# recorded series exist
curl -s 'localhost:9090/api/v1/query?query=job:http_error_rate:ratio5m' | jq '.data.result'
# trigger sustained errors
curl -s -XPOST localhost:8080/admin/chaos -d '{"enabled":true,"error_ratio":0.3}'
# watch: pending, then firing after 5m
watch -n5 'curl -s "localhost:9090/api/v1/query?query=ALERTS" | jq -c ".data.result[] | {name: .metric.alertname, state: .metric.alertstate}"'
# notification lands
docker compose logs -f webhook-sink
```

- [ ] `make check-config` passes for both files
- [ ] All 3 recorded series appear in Prometheus and update on their `interval`
- [ ] `localhost:9090/rules` shows all rules healthy; `/alerts` lists the 3 alerts
- [ ] `ApiDown` fires within ~1m of stopping the app container; resolves on restart
- [ ] Sustained `error_ratio:0.3` → `HighErrorRate` goes `pending` then `firing` after 5m
- [ ] `webhook-sink` logs the alert (firing) and again on resolve (`send_resolved`)
- [ ] Alertmanager UI (`localhost:9093`) shows the alert grouped by `alertname`
- [ ] Error % panel repointed to the recorded series, still renders
- [ ] "Active alerts" table + alert annotation on the dashboard
- [ ] `docs/07-rules-alerting.md` written
- [ ] Committed, tagged `phase-7`

## Traps to notice

- **Alert stuck `pending` forever** — your `for:` is longer than you think, or
  the expr flaps below the threshold between evaluations. Check the raw expr
  value on `/graph`.
- **`HighLatencyP95` won't fire from chaos latency alone** — chaos adds latency
  to `/slow` too, but the *aggregate* P95 needs enough slow traffic to cross
  1s. Use `{"latency_ms":1500}` and give it the full 10m.
- **Rules file not loaded** — `/etc/prometheus/rules/*.yml` glob needs the dir
  mounted; check `localhost:9090/rules` is non-empty and
  `docker compose logs prometheus | grep -i rule`.
- **Nothing reaches the sink** — check `localhost:9093` first (did Prometheus
  deliver to AM?), then the sink logs (did AM deliver onward?). The split tells
  you which hop failed.
- **`amtool check-config` fails on `repeat_interval < group_interval`** — keep
  `repeat_interval` ≥ `group_interval`.
