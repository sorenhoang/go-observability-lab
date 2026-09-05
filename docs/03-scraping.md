# Scraping With Prometheus

Phase 3 runs the API and Prometheus together in Docker Compose. The API still
does not know Prometheus exists; it exposes `/metrics`, and Prometheus pulls
that endpoint on a schedule. By default Compose publishes the API on host port
`8080` and Prometheus on `9090`; set `API_HOST_PORT` or
`PROMETHEUS_HOST_PORT` only when those host ports are already occupied.

## Pull Model

Prometheus scrapes targets instead of receiving pushed metrics. In this lab the
only target is the API container at `app:8080`, using the default `/metrics`
path. The Compose service name matters: from inside the Prometheus container,
`localhost:8080` would mean Prometheus itself, not the API container.

## Target And Job

A target is one scrape endpoint. A job is the logical group Prometheus attaches
to targets that serve the same role. This phase has one job, `api`, and one
target, `app:8080`. Prometheus adds the synthetic `up` metric for every target:
`1` means the most recent scrape succeeded, and `0` means it failed.

## Scrape Interval And Rate Windows

`prometheus/prometheus.yml` sets `scrape_interval: 5s` for fast lab feedback. A
PromQL rate window needs several samples to be meaningful; a `[5m]` window with
a `5s` interval gives about 60 samples, while even a short `[1m]` check gets
about 12 samples once the system has warmed up.

## Load Generator

The `k6` service is behind the `load` Compose profile, so `make up` starts only
the API and Prometheus. `make load` runs k6 as a one-off container on the
existing Compose network, which avoids recreating the app while traffic is
being tested. The script sends mixed read-heavy traffic: mostly `/users` and
`/products`, occasional `/orders`, and rare `/slow` and `/error` calls. That
keeps request-rate graphs moving without letting simulator endpoints dominate
the shape.

## Data Persistence

Prometheus writes its TSDB to a named volume (`prometheus_data`), so scraped
history survives `make down` / `make up`. To start completely fresh, run
`docker compose down -v`.

## Healthcheck Decision

This phase intentionally skips a Docker healthcheck. The final image is
distroless/static, so a shell or `curl` healthcheck would not run. Adding an
`/api -healthcheck` mode would work, but there is no database or external
dependency to validate yet. Until Phase 8, it is acceptable for Prometheus to
scrape a starting container once or twice and show `up=0` before the API is
ready.
