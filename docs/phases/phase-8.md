# Phase 8 — Infrastructure observability

**Status: DONE (with 3 known gaps — see "Known issues").** Real backing systems
land: Postgres (via a `database/sql` store), Redis (read-through products cache),
Kafka (best-effort order events + a `cmd/consumer` binary), plus `node-exporter`
and `cadvisor`. Each backing system gets a sidecar **exporter**; Prometheus now
scrapes 9 jobs. One `infra.json` dashboard, 4 rows. Two new alerts wired into the
Phase 7 chain.

## Objective

Everything through Phase 7 measured *our code*. Real incidents usually start one
layer down — the database is out of connections, the cache is cold, the consumer
is lagging, the host is out of disk. This phase adds those systems and wires up
the standard pattern for observing them: a sidecar **exporter** that scrapes the
system's own stats interface and republishes it in Prometheus format.

By the end you can answer, from Grafana alone: *is the problem in the app, or in
something the app depends on?*

## Concepts to internalize

- **The exporter pattern.** You cannot add a Go middleware to Postgres. Instead
  you run a small process next to it that speaks the system's native stats
  protocol (`pg_stat_*` views, the Redis `INFO` command, Kafka's admin API, the
  kernel's `/proc`) and exposes `/metrics`. Prometheus scrapes the exporter, not
  the system. The exporter is stateless and disposable — restart it freely.
- **Two viewpoints on one subsystem.** For each dependency you get an
  *app-side* metric (how *we* experience it: `db_query_duration_seconds`,
  `cache_hits_total`) and an *exporter* metric (how the system sees itself:
  `pg_stat_database_blks_hit`, `redis_keyspace_hits_total`). Neither is
  redundant. App-side tells you the user impact; exporter tells you the cause.
- **RED for services, USE for resources.** Your HTTP layer is a *service* —
  Rate, Errors, Duration. A database connection pool, a disk, a CPU is a
  *resource* — **U**tilization, **S**aturation, **E**rrors. `db_pool_in_use / 10`
  (utilization) and `rate(db_pool_wait_count_total[5m])` (saturation) are the USE
  view of the pool.
- **Consumer lag is the canonical async signal.** For anything queue-shaped,
  `messages produced − messages consumed`, expressed as `kafka_consumergroup_lag`,
  is the one number that tells you if the async side is keeping up. A flat
  non-zero lag is fine; a rising lag is an incident.
- **Exporters bring cardinality.** `cadvisor` alone emits per-container,
  per-interface, per-mountpoint series. The `cadvisor` scrape job carries
  `metric_relabel_configs` that drop the filesystem / blkio / task-state / unlabelled
  series before they hit the TSDB — the cardinality-hygiene exercise of the phase.
- **A self-healing client plus a one-shot startup probe is an anti-pattern.**
  `go-redis` and `kafka-go` both reconnect on their own. Gating the feature on a
  single startup dial (and then never retrying) turns a transient boot-order blip
  into a permanent outage. The per-request fallback is what actually keeps the
  service up. (This lab currently gets this *wrong* for Kafka and Redis — see
  Known issues; it is left in as a teaching artefact.)

---

## What shipped

### 8a — Postgres

| Piece | Detail |
|-------|--------|
| `postgres/initdb/01-schema.sql` | `users`, `products`, `orders` (FK + `NUMERIC` + `CHECK` — the lab's own DB, not house schema rules), seeded. Mounted at `/docker-entrypoint-initdb.d/`, runs once on first boot. |
| `internal/store/store.go` | `database/sql` + `jackc/pgx/v5/stdlib` driver. `Open()` sets `MaxOpenConns=10`, `MaxIdleConns=5`, `ConnMaxLifetime=30m`, pings, registers the pool collector. Methods: `Users`, `Products`, `ProductExists`, `CreateOrder`. Every call goes through `timed()`. |
| `db_query_duration_seconds{query,status}` | Histogram. `query` is a fixed call-site name (`users.list`, `orders.insert`, …) — **never** SQL text. |
| `db_pool_*` | Custom `prometheus.Collector` reading `sql.DB.Stats()` at scrape time: `open_connections`, `in_use_connections`, `idle_connections`, `wait_count_total`, `wait_duration_seconds_total`. |
| Config | `API_DATABASE_URL` (default `postgres://lab:lab@postgres:5432/lab?sslmode=disable`). The DB is the source of truth — if it is unreachable at startup the API logs and exits non-zero. |
| Compose | `postgres:17.2` (healthcheck `pg_isready`), `postgres-exporter:v0.16.0`. `app` waits on `postgres: service_healthy`. |
| Scrape | `job_name: postgres` → `postgres-exporter:9187`. |
| Handlers | `handleUsers`/`handleProducts`/`handleCreateOrder` now hit the store with `r.Context()`; a client disconnect cancels the query. In-memory slices deleted. |

### 8b — Redis

| Piece | Detail |
|-------|--------|
| `internal/cache/cache.go` | `redis/go-redis/v9`. Read-through cache for `GET /products`, key `products`, 30s TTL. A Redis error (not `redis.Nil`) → `cache_errors_total++`, logged, **fall through to the DB** — never a 5xx. |
| `cache_hits_total{result}` | `result="hit"|"miss"`. Hit ratio: `rate(cache_hits_total{result="hit"}[5m]) / rate(cache_hits_total[5m])`. |
| `cache_errors_total` | Every Redis GET/SET/decode failure. |
| Config | `API_REDIS_ADDR` (default `redis:6379`). Redis unreachable at startup → cache runs *disabled* (every call a straight DB read). |
| Compose | `redis:7.4-alpine` (`--save "" --maxmemory 64mb --maxmemory-policy allkeys-lru`), `redis-exporter:v1.67.0`. |
| Scrape | `job_name: redis` → `redis-exporter:9121`. |

### 8c — Kafka

| Piece | Detail |
|-------|--------|
| `internal/events/producer.go` | `segmentio/kafka-go`. On a committed order, publish `{order_id, product_id, qty, ts}` to topic `orders` in a background goroutine (`context.WithoutCancel` so the HTTP request finishing doesn't kill the publish). Failure → `orders_publish_errors_total++`, **no** change to the HTTP response — Postgres already has the order. |
| `cmd/consumer/main.go` | Third binary (image builds `./cmd/...`). Joins group `order-processors`, reads `orders`, sleeps `CONSUMER_DELAY_MS` (default 50) to simulate work, exposes its own `/metrics` on `:9002`. Metrics: `orders_consumed_total`, `order_processing_duration_seconds`. |
| `orders_published_total` | App counter — events successfully written to Kafka. |
| Config | App: `API_KAFKA_BROKERS` (default `kafka:9092`). Consumer: `CONSUMER_KAFKA_BROKERS`, `CONSUMER_DELAY_MS`. |
| Compose | `apache/kafka:3.9.0` (KRaft, single node, `REPLICATION_FACTOR: 1`), `consumer`, `kafka-exporter:v1.8.0` (`--group.filter=order-processors`). |
| Scrape | `job_name: kafka` → `kafka-exporter:9308`; `job_name: consumer` → `consumer:9002`. |
| Alert | `OrderConsumerLagging`: `sum(kafka_consumergroup_lag{consumergroup="order-processors"}) > 500` for `3m`, warning → webhook sink. |

### 8d — Host / container

| Piece | Detail |
|-------|--------|
| Compose | `node-exporter:v1.8.2` (`--path.rootfs=/host`, `pid: host`, `/:/host:ro`), `cadvisor:v0.49.1` (privileged, `/dev/kmsg`, `/sys` + `/var/lib/docker` mounts). No app changes. |
| Scrape | `job_name: node` → `node-exporter:9100`; `job_name: cadvisor` → `cadvisor:8080` with `metric_relabel_configs` dropping `container_(network_tcp_usage_total|tasks_state|fs_.*|blkio_.*)` and any series with an empty `container_label_com_docker_compose_service`. |
| Alert | `HostDiskFillingUp`: `predict_linear(node_filesystem_avail_bytes{mountpoint="/host"}[1h], 4*3600) < 0` for `10m`, warning. |

### Cross-cutting

- `internal/api/router.go` — `Handlers` now depends on three interfaces
  (`dataStore`, `productCache`, `orderPublisher`); `deps_test.go` provides fakes,
  so `make test` stays hermetic with no infra running.
- `internal/store/store_test.go` and `internal/cache/cache_test.go` — integration
  tests that `t.Skip()` unless `API_DATABASE_URL` / `API_REDIS_ADDR` is set.
- `grafana/dashboards/infra.json` (uid `infra`) — auto-provisioned, 4 rows
  (Postgres / Redis / Kafka / Host & Containers).
- `Makefile` — `make dash-infra`. `Dockerfile` — `EXPOSE 8080 9000 9002`.
- New deps: `jackc/pgx/v5`, `redis/go-redis/v9`, `segmentio/kafka-go`.

---

## Known issues

These shipped as-is and are documented rather than hidden. Fixing them is a good
follow-up exercise.

1. **Kafka publishing is disabled on a cold `make up`.**
   `events.NewProducerIfAvailable` does a single 2s dial with no retry, and
   compose only gates `app` on `kafka: service_started` (process up, not broker
   ready — KRaft needs ~15–25s). The app loses the race, logs *"order publishing
   disabled"*, and `p.enabled` stays false until `docker compose restart app`.
   `kafka-go`'s `Writer` reconnects on its own, so the fix is to always build the
   writer when brokers are set and let `orders_publish_errors_total` carry the
   gap — or add a Kafka healthcheck and `condition: service_healthy`.
   **Workaround for now:** `docker compose restart app` once the stack is up.

2. **`HostDiskFillingUp` and the disk panel match nothing.**
   With `--path.rootfs=/host`, node-exporter *strips* the `/host` prefix from
   `mountpoint` labels, so `node_filesystem_avail_bytes{mountpoint="/host"}` is
   always empty. Point the rule and panel at `mountpoint="/"` (or the real data
   mount, `/var/lib/docker` on Docker Desktop), or drop `--path.rootfs`.

3. **Redis startup Ping permanently disables the cache.**
   Same anti-pattern as #1 — a transient blip at boot (or Redis restarting later)
   drops the cache to *disabled* for the life of the process, even though the
   per-request path already falls through to Postgres correctly. Make the startup
   Ping advisory (log, stay enabled).

Minor: `store.Users`/`Products` return a `nil` slice on an empty table → JSON
`null` not `[]` (seed data hides it); the publish goroutine is unbounded under a
spike; on a Redis outage the code still attempts `SET` after a failed `GET`
(double log line per request).

---

## Verify

### Static

```sh
make check          # fmt + vet + test — passes with NO infra running
make check-config   # promtool check rules + amtool — 5 alert rules
```

### Stack

```sh
make up             # ~12 containers; KRaft Kafka needs ~30s
docker compose ps   # all Up; postgres "healthy"
open http://localhost:9090/targets   # 9 jobs UP: api alertmanager postgres redis
                                     #   kafka consumer node cadvisor
make load
make dash-infra
```

### 8a Postgres

```sh
curl -s localhost:8080/users | jq
curl -s -XPOST localhost:8080/orders -d '{"product_id":1,"qty":2}' | jq
curl -s localhost:8080/metrics | grep -E 'db_query_duration_seconds_count|db_pool_'
curl -s 'localhost:9090/api/v1/query?query=pg_up' | jq '.data.result[0].value[1]'   # "1"
```
Dashboard **Postgres** row: query p95 by name; pool-in-use climbs toward 10 under
load. Saturation drill: set `SetMaxOpenConns(2)` in `store.go`, `make up`,
`make load` → `rate(db_pool_wait_count_total[1m])` and p95 move together.

### 8b Redis

```sh
curl -s localhost:8080/products >/dev/null   # miss
curl -s localhost:8080/products >/dev/null   # hit
curl -s localhost:8080/metrics | grep cache_hits_total
docker compose stop redis
curl -s localhost:8080/products | jq length  # still 3, no 5xx
curl -s localhost:8080/metrics | grep cache_errors_total
docker compose start redis
```

### 8c Kafka

```sh
docker compose restart app          # workaround for Known issue #1
curl -s -XPOST localhost:8080/orders -d '{"product_id":1,"qty":1}'
curl -s localhost:8080/metrics | grep orders_published_total       # > 0
docker compose logs consumer | tail
curl -s 'localhost:9090/api/v1/query?query=sum(kafka_consumergroup_lag{consumergroup="order-processors"})' | jq

# lag -> alert -> sink
CONSUMER_DELAY_MS=400 docker compose up -d consumer
make spike
watch -n5 'curl -s "localhost:9090/api/v1/query?query=ALERTS{alertname=\"OrderConsumerLagging\"}" | jq -c ".data.result[].metric.alertstate"'
docker compose logs -f webhook-sink     # firing, then resolved
CONSUMER_DELAY_MS=50 docker compose up -d consumer

# kafka down: orders still 201
docker compose stop kafka
curl -s -XPOST localhost:8080/orders -d '{"product_id":1,"qty":1}' -w '%{http_code}\n'
curl -s localhost:8080/metrics | grep orders_publish_errors_total
docker compose start kafka
```

### 8d Host / container

```sh
curl -s 'localhost:9090/api/v1/query?query=count({job="cadvisor"})' | jq   # trimmed
```
Dashboard **Host / Containers** row: per-container memory & CPU keyed by compose
service name. Disk panel + `HostDiskFillingUp` are empty — Known issue #2. On
Docker Desktop, node-exporter / cadvisor report the Linux VM, not macOS; cadvisor
may not start at all — note it and move on.

## Definition of Done

- [x] All four sub-phases shipped and merged (PR #9)
- [x] `/targets` shows 9 jobs UP (cadvisor best-effort on Docker Desktop)
- [x] `make test` passes with no infra running (integration tests skip)
- [x] `infra.json` auto-provisioned, 4 rows
- [x] `db_query_duration_seconds` uses a bounded `query` label
- [x] `OrderConsumerLagging` fires to the webhook sink under a throttled consumer
- [x] cadvisor cardinality trimmed via `metric_relabel_configs`
- [x] `docs/08-infrastructure.md` written
- [ ] Known issues #1–#3 fixed (follow-up)

## Traps to notice

- **`make test` must not need Docker.** Every infra `*_test.go` guards on its env
  var and `t.Skip()`s. Hermetic unit suite, opt-in integration.
- **`app` starting before Postgres is ready** — without
  `condition: service_healthy` the app crash-loops until Postgres is up, which
  *looks* fine afterward and hides the ordering bug.
- **A self-healing client + a one-shot startup probe** — see Known issues #1/#3.
  The client already reconnects; don't gate the feature on a single dial.
- **`query` / `container` label cardinality** — the `query` label is a fixed enum
  of call-site names, never SQL. The cadvisor job *must* have
  `metric_relabel_configs` or Prometheus's series count jumps ~10x from one
  exporter.
- **Kafka KRaft single-node** needs `KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: 1`
  (default 3) or `__consumer_offsets` never creates and the group never commits —
  lag reads as zero or garbage.
- **Redis error treated as a request error** — a cache is an optimization. Redis
  down must degrade to "slow but working", never to 5xx.
- **`--path.rootfs` rewrites mountpoint labels** — Known issue #2. Query the
  label node-exporter actually emits, not the container mount path.
- **Exporter version skew** — every exporter image tag is pinned;
  `redis_exporter` / `postgres_exporter` rename metrics between minors and an
  unpinned `latest` breaks a panel months later.
