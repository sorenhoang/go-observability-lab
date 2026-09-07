# go-observability-lab

A learning lab for **production-style observability** with Prometheus and Grafana,
built around a deliberately boring Go HTTP API.

The API is a prop. The real deliverable is hands-on fluency with metrics, PromQL,
dashboards, and alerting. Effort is intentionally weighted **~20% application code /
~80% observability**.

## Approach

- **Explicit instrumentation.** RED metrics are wired by hand in a custom
  `net/http` middleware, not hidden behind a framework or `promhttp` one-liners.
  You should be able to explain what every metric does and what problem it
  diagnoses.
- **One concept cluster per phase.** Each phase is small, independently runnable,
  and tagged in git (`phase-0` … `phase-8`).
- **Docker Compose only.** No Kubernetes.

## How to run

### Phase 0–2 (local Go)

```sh
cp .env.example .env   # optional: your local tunable overrides
make run                # start the API on :8080 (loads .env if present)
curl localhost:8080/health
```

See [docs/config.md](docs/config.md) for how config/env files work and how
per-environment profiles fit in.

> **From Phase 8 on, the API needs Postgres.** Bare `make run` exits unless it
> can reach a database at `API_DATABASE_URL`. Either run the full stack with
> `make up`, or start just the backing store (`docker compose up -d postgres`)
> and point `.env` at `postgres://lab:lab@localhost:5432/lab?sslmode=disable`.

### Phase 3+ (Docker Compose)

```sh
make up            # app + prometheus + grafana + (P7) alertmanager + (P8) postgres/redis/kafka + exporters
make load          # start the k6 load generator (arrives in Phase 3)
make spike         # ramping traffic-spike scenario (arrives in Phase 6)
make dash          # open the RED dashboard  ·  make dash-infra for the infra one
make down
```

| Service      | URL                     | Arrives |
|--------------|-------------------------|---------|
| API          | http://localhost:8080   | P1      |
| API metrics  | http://localhost:8080/metrics | P2 |
| Prometheus   | http://localhost:9090   | P3      |
| Grafana / RED dashboard | http://localhost:3000/d/red | P5 |
| Alertmanager | http://localhost:9093   | P7      |
| Infra dashboard | http://localhost:3000/d/infra | P8 |

Phase 8 also runs Postgres, Redis, a single-node Kafka, an order `consumer`, and
five exporters (`postgres` / `redis` / `kafka` / `node` / `cadvisor`) — all
scraped by Prometheus, none with a UI of their own. `docker compose ps` lists
them; `http://localhost:9090/targets` shows all 9 scrape jobs.

## Phase checklist

- [x] **P0** Repository bootstrap — skeleton, Makefile, docs, `make run` works
- [x] **P1** Simple Go API — 6 endpoints, in-memory data, `/slow` + `/error` behave
- [x] **P2** Prometheus instrumentation — 4 RED metrics by hand, `/metrics` live, route templates
- [x] **P3** Prometheus server + scraping + load generator — target UP, live traffic, `up` works
- [x] **P4** PromQL practice — cheatsheet filled by doing; write error-rate / P95 unaided
- [x] **P5** Grafana RED dashboard — provisioned, all panels + latency heatmap + `$route`
- [x] **P6** Failure simulation — fault injection + runtime metrics + failure playbook
- [x] **P7** Recording rules + alerting — Alertmanager + webhook sink, alerts fire
- [x] **P8** Infrastructure observability — Postgres / Redis / Kafka / host+container exporters ([3 known gaps](docs/phases/phase-8.md#known-issues))

## Docs

| Doc | What |
|-----|------|
| [docs/roadmap.md](docs/roadmap.md) | Full phased roadmap |
| [docs/config.md](docs/config.md) | Env vars, `.env` files, per-environment profiles |
| [docs/00-intro.md](docs/00-intro.md) | Why observability; why this lab is metrics-only |
| [docs/glossary.md](docs/glossary.md) | Counter, gauge, histogram, cardinality, scrape, … |
| [docs/phases/](docs/phases/) | Detailed build guide per phase |
