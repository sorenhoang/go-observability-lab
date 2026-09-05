# Phase 3 — Prometheus server, scraping, load generator

**Status: DONE.** `docker compose up` runs app + Prometheus; `make load` drives
a weighted k6 script. Verified live: target UP, `up` flips on stop/start,
per-route request rate matches the script's traffic shape. Healthcheck
deliberately skipped (distroless image, no dependency worth checking yet).

## Objective

Get a real Prometheus server scraping your app in Docker Compose, with a load
generator so the target isn't flat and silent. No PromQL mastery yet (Phase 4),
no Grafana yet (Phase 5) — just: is the pull model actually working, end to end.

## Concepts to internalize

- **The pull model.** Your app doesn't know Prometheus exists. It exposes
  `/metrics`; Prometheus fetches it. Nothing is pushed anywhere.
- **Target / job.** A *target* is one scrape endpoint (host:port + path). A
  *job* is a named group of targets serving the same purpose — `job="api"`.
- **`scrape_interval`.** How often Prometheus scrapes. This lab uses `5s`
  (fast feedback for a lab; production is usually `15s`-`60s`).
- **`up`.** Prometheus synthesizes this per target: `1` if the last scrape
  succeeded, `0` if not. Your first availability signal, and it costs you
  nothing — you didn't write it.
- **Why the load generator matters now, not later.** A target with zero
  traffic produces flat, empty graphs. You cannot learn PromQL, build a
  dashboard, or see a fault simulation land on a chart if there's nothing
  moving. Load generation is pulled forward to this phase specifically so
  every phase after this one has real, continuous data to look at.

## What to build

### 1. `Dockerfile` — multi-stage, static binary

```dockerfile
# syntax=docker/dockerfile:1
FROM golang:1.26 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/api /api
EXPOSE 8080
ENTRYPOINT ["/api"]
```

**Why `distroless/static`, not `alpine` or `scratch`:** `CGO_ENABLED=0` gives a
fully static binary, so there's no libc to miss — `distroless/static` has no
shell, no package manager, nothing an attacker could use if they got in, and
it's smaller than `alpine`. `scratch` would also work; distroless adds CA
certs and `/etc/passwd`, which you'd want the moment this app makes an
outbound HTTPS call (it doesn't yet, but the habit is worth having).

**Gotcha:** copy `go.mod`/`go.sum` and run `go mod download` *before* `COPY . .`
— Docker layer caching means editing a `.go` file won't re-download modules
every build.

### 2. `docker-compose.yml` — app + prometheus, load generator opt-in

```yaml
services:
  app:
    build: .
    ports:
      - "8080:8080"
    environment:
      API_ADDR: ":8080"
    healthcheck:
      test: ["CMD", "/api", "-healthcheck"] # see note below
      interval: 5s
      timeout: 2s
      retries: 3

  prometheus:
    image: prom/prometheus:v3.7.3
    ports:
      - "9090:9090"
    volumes:
      - ./prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
    depends_on:
      app:
        condition: service_healthy

  k6:
    image: grafana/k6:latest
    profiles: ["load"] # opt-in: `docker compose --profile load up`
    volumes:
      - ./loadgen:/scripts:ro
    environment:
      BASE_URL: "http://app:8080"
    command: ["run", "/scripts/script.js"]
    depends_on:
      app:
        condition: service_healthy
```

**Healthcheck note:** distroless has no shell, so `CMD-SHELL curl ...` won't
work (no curl, no shell). Two options: (a) add a tiny `-healthcheck` flag to
`main.go` that does an in-process HTTP GET to `/health` and exits 0/1 — a few
lines, no new dependency; or (b) skip the container healthcheck and rely on
Compose's `depends_on` without a condition, accepting that Prometheus might
scrape a not-yet-ready app for the first few seconds (harmless — `up` just
reads `0` until it succeeds). **I'd pick (b) for this lab** — a real
healthcheck is Phase 8 territory once there's a DB connection worth checking.
Your call; note whichever you choose in `docs/03-scraping.md`.

**Why `k6` is a Compose *profile*, not always-on:** you want `docker compose up`
to start the observability stack instantly; load is something you turn on
deliberately with `make load` (or `--profile load`), not a permanent background
process eating your terminal's logs.

### 3. `prometheus/prometheus.yml`

```yaml
global:
  scrape_interval: 5s

scrape_configs:
  - job_name: api
    static_configs:
      - targets: ["app:8080"]
```

That's the entire file for this phase. `rule_files` and `alerting` blocks
arrive in Phase 7.

### 4. `loadgen/script.js` — steady mixed traffic

```javascript
import http from 'k6/http';
import { sleep } from 'k6';

const BASE_URL = __ENV.BASE_URL || 'http://localhost:8080';

export const options = {
  scenarios: {
    steady: {
      executor: 'constant-vus',
      vus: 10,
      duration: '10m',
    },
  },
};

// Weighted so /users and /products dominate, /orders occasional, /slow and
// /error rare — roughly what a real read-heavy API's traffic shape looks
// like, and it keeps /slow and /error from swamping the RPS graph.
export default function () {
  const r = Math.random();
  if (r < 0.4) {
    http.get(`${BASE_URL}/users`);
  } else if (r < 0.7) {
    http.get(`${BASE_URL}/products`);
  } else if (r < 0.85) {
    http.post(`${BASE_URL}/orders`, JSON.stringify({ product_id: 1, qty: 1 }), {
      headers: { 'Content-Type': 'application/json' },
    });
  } else if (r < 0.95) {
    http.get(`${BASE_URL}/slow`);
  } else {
    http.get(`${BASE_URL}/error`);
  }
  sleep(0.5);
}
```

10 VUs × ~0.5s think time ≈ ~20 req/s — enough to see continuous movement on
every panel without flooding your terminal.

### 5. Makefile targets

```makefile
up: ## Start app + prometheus
	docker compose up --build -d

down: ## Stop everything
	docker compose down

load: ## Start the k6 load generator against the running stack
	docker compose --profile load up k6
```

### 6. `docs/03-scraping.md`

Cover: the pull model, target vs job, `scrape_interval` vs rate window (a
`[5m]` rate window needs at least ~4 scrapes to be meaningful — `5s` interval
gives you 60 samples in 5m, plenty), and your healthcheck decision from step 2.

---

## Definition of Done

```sh
make up
# wait ~10s
open http://localhost:9090/targets   # or curl -s localhost:9090/api/v1/targets | jq
```

- [ ] Prometheus **Targets** page shows `job="api"` as **UP**
- [ ] `curl -s 'localhost:9090/api/v1/query?query=up'` returns `value: [..., "1"]`
- [ ] `make load` starts k6; `curl -s 'localhost:9090/api/v1/query?query=rate(http_requests_total[1m])'` returns a non-zero value within ~30s
- [ ] `docker compose down && make up` — target comes back up on its own within one scrape interval
- [ ] Stopping just the app (`docker stop <container>`) flips `up` to `0` within ~15s; starting it again flips it back
- [ ] `docs/03-scraping.md` written, including your healthcheck decision
- [ ] Committed, tagged `phase-3`

## Traps to notice

- If you see `up == 0` and can't figure out why, check `docker compose logs prometheus` before anything else — a wrong target hostname (`localhost:8080` instead of `app:8080`, the *service name* Compose gives containers) is the #1 cause. Containers reach each other by service name, never `localhost`.
- A `scrape_interval` slower than your rate-window-divided-by-4 gives sparse, jumpy graphs later. `5s`/`[5m]` is already the safe ratio the whole lab standardizes on (see `docs/roadmap.md`).
