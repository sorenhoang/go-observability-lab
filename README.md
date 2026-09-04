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
make run          # start the API on :8080
curl localhost:8080/health
```

### Phase 3+ (Docker Compose)

```sh
make up           # app + prometheus + grafana (arrives in Phase 3/5)
make load         # start the k6 load generator (arrives in Phase 3)
make down
```

| Service      | URL                     | Arrives |
|--------------|-------------------------|---------|
| API          | http://localhost:8080   | P1      |
| API metrics  | http://localhost:8080/metrics | P2 |
| Prometheus   | http://localhost:9090   | P3      |
| Grafana      | http://localhost:3000   | P5      |
| Alertmanager | http://localhost:9093   | P7      |

## Phase checklist

- [ ] **P0** Repository bootstrap — skeleton, Makefile, docs, `make run` works
- [ ] **P1** Simple Go API — 6 endpoints, in-memory data, `/slow` + `/error` behave
- [ ] **P2** Prometheus instrumentation — 4 RED metrics by hand, `/metrics` live, route templates
- [ ] **P3** Prometheus server + scraping + load generator — target UP, live traffic, `up` works
- [ ] **P4** PromQL practice — cheatsheet filled by doing; write error-rate / P95 unaided
- [ ] **P5** Grafana RED dashboard — provisioned, all panels + latency heatmap + `$route`
- [ ] **P6** Failure simulation — fault injection + runtime metrics + failure playbook
- [ ] **P7** Recording rules + alerting — Alertmanager + webhook sink, alerts fire
- [ ] **P8** Infrastructure observability — exporters (Postgres / Redis / Kafka / host)

## Docs

| Doc | What |
|-----|------|
| [docs/roadmap.md](docs/roadmap.md) | Full phased roadmap |
| [docs/00-intro.md](docs/00-intro.md) | Why observability; why this lab is metrics-only |
| [docs/glossary.md](docs/glossary.md) | Counter, gauge, histogram, cardinality, scrape, … |
| [docs/phases/](docs/phases/) | Detailed build guide per phase |
