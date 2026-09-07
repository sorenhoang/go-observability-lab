# Phase 8 — Infrastructure observability

**Status: PLANNED.** Four modular sub-phases, each independently runnable and
tagged. Adds real backing systems (Postgres, Redis, Kafka) plus host/container
metrics, and instruments them the way you instrument things you can't put
middleware inside: **exporters**.

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
  *resource* — **U**tilization, **S**aturation, **E**rrors. `pg pool 95/100 in
  use` (utilization) and `12 connections waiting` (saturation) are the USE view.
- **Consumer lag is the canonical async signal.** For anything queue-shaped,
  `messages produced − messages consumed`, expressed as `kafka_consumergroup_lag`,
  is the one number that tells you if the async side is keeping up. A flat
  non-zero lag is fine; a rising lag is an incident.
- **Exporters bring cardinality.** `postgres_exporter` emits per-database and
  optionally per-table series; `cadvisor` emits per-container, per-interface,
  per-mountpoint. Left unfiltered this is where a lab Prometheus first feels
  slow. Scrape only what a panel or alert uses.

---

## Sub-phase 8a — Postgres

Replace the in-memory fakes with a real store, measure our own queries, and add
`postgres_exporter`.

### What to build

**1. `postgres/initdb/01-schema.sql`** — mounted into the container's
`/docker-entrypoint-initdb.d/`, so Postgres runs it once on first boot. No
migration tool for a lab.

```sql
CREATE TABLE users (
  id   BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name TEXT NOT NULL
);
CREATE TABLE products (
  id    BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name  TEXT NOT NULL,
  price NUMERIC(10,2) NOT NULL
);
CREATE TABLE orders (
  id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  product_id BIGINT NOT NULL REFERENCES products(id),
  qty        INT NOT NULL CHECK (qty > 0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
INSERT INTO users (name) VALUES ('Ada'), ('Alan'), ('Grace');
INSERT INTO products (name, price) VALUES ('Widget', 9.99), ('Gadget', 19.99), ('Gizmo', 4.50);
```

(This is the lab's own DB, not a service following the house schema rules — FKs
and `NUMERIC` are fine here and make the exporter's constraint/rollback metrics
more interesting.)

**2. `internal/store/store.go`** — thin `database/sql` layer, pgx as the driver.

```go
import (
    "database/sql"
    _ "github.com/jackc/pgx/v5/stdlib"
)

type Store struct{ db *sql.DB }

func Open(ctx context.Context, dsn string) (*Store, error) {
    db, err := sql.Open("pgx", dsn)
    if err != nil { return nil, err }
    db.SetMaxOpenConns(10)          // small on purpose — pool saturation is a lab exercise
    db.SetMaxIdleConns(5)
    db.SetConnMaxLifetime(30 * time.Minute)
    if err := db.PingContext(ctx); err != nil { return nil, err }
    return &Store{db: db}, nil
}

func (s *Store) Users(ctx context.Context) ([]User, error)      { ... }
func (s *Store) Products(ctx context.Context) ([]Product, error) { ... }
func (s *Store) ProductExists(ctx context.Context, id int) (bool, error) { ... }
func (s *Store) CreateOrder(ctx context.Context, productID, qty int) (int64, error) { ... }
```

New dependency: `github.com/jackc/pgx/v5` — justified, you cannot speak the
Postgres wire protocol in a few lines. `database/sql` over `pgxpool` keeps the
driver seam boring and gives `sql.DBStats` for free (see step 4).

**3. Rewire handlers.** `Handlers` gets a `store *store.Store`. `handleUsers`,
`handleProducts`, `handleCreateOrder` call the store instead of ranging over
package slices. Handlers now take `r.Context()` through to the query so a client
disconnect cancels the DB call. Delete the `users` / `products` slices and
`productExists`.

**4. `db_query_duration_seconds`** — a histogram the store records around every
query, plus the pool gauges from `sql.DBStats`.

```go
// in internal/metrics
dbQueryDuration *prometheus.HistogramVec // labels: query, status ("ok"|"error")
// buckets: .001 .0025 .005 .01 .025 .05 .1 .25 .5 1

// pool: register a custom collector that reads s.db.Stats() at scrape time
//   db_pool_open_connections
//   db_pool_in_use_connections
//   db_pool_idle_connections
//   db_pool_wait_count_total
//   db_pool_wait_duration_seconds_total
```

`query` label is a fixed short string per call site (`"users.list"`,
`"orders.insert"`) — never the SQL text, never interpolated values. Bounded set,
same rule as the `route` label in Phase 2.

Wrap each store method:

```go
func (s *Store) timed(ctx context.Context, name string, fn func(context.Context) error) error {
    start := time.Now()
    err := fn(ctx)
    status := "ok"
    if err != nil { status = "error" }
    s.metrics.ObserveQuery(name, status, time.Since(start))
    return err
}
```

**5. Config.** Add `DatabaseURL string` (`API_DATABASE_URL`, default
`postgres://lab:lab@postgres:5432/lab?sslmode=disable`). `main.go` opens the
store before building the router; if the DB is unreachable at startup, log and
exit non-zero — the app genuinely can't serve now.

**6. `docker-compose.yml`** — `postgres` + `postgres-exporter`.

```yaml
  postgres:
    image: postgres:17.2
    environment:
      POSTGRES_USER: lab
      POSTGRES_PASSWORD: lab
      POSTGRES_DB: lab
    volumes:
      - ./postgres/initdb:/docker-entrypoint-initdb.d:ro
      - postgres_data:/var/lib/postgresql/data
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U lab -d lab"]
      interval: 5s
      timeout: 3s
      retries: 10

  postgres-exporter:
    image: quay.io/prometheuscommunity/postgres-exporter:v0.16.0
    environment:
      DATA_SOURCE_NAME: "postgresql://lab:lab@postgres:5432/lab?sslmode=disable"
    depends_on:
      postgres:
        condition: service_healthy
```

`app` now depends on `postgres` being healthy:

```yaml
  app:
    depends_on:
      postgres:
        condition: service_healthy
```

**7. `prometheus/prometheus.yml`** — add a scrape job:

```yaml
  - job_name: postgres
    static_configs:
      - targets: ["postgres-exporter:9187"]
```

**8. Dashboard — `grafana/dashboards/infra.json`**, "Postgres" row:

| Panel | Query | Notes |
|-------|-------|-------|
| Query p95 (app-side) | `histogram_quantile(0.95, sum by (le, query) (rate(db_query_duration_seconds_bucket[$__rate_interval])))` | our experience |
| Pool in use / open | `db_pool_in_use_connections` / `db_pool_open_connections` | utilization — climbs toward 10 under load |
| Pool waiters | `rate(db_pool_wait_count_total[$__rate_interval])` | saturation — non-zero means the pool is the bottleneck |
| Commits vs rollbacks | `rate(pg_stat_database_xact_commit{datname="lab"}[$__rate_interval])`, `..._xact_rollback...` | exporter view |
| Cache hit ratio | `rate(pg_stat_database_blks_hit{datname="lab"}[5m]) / (rate(pg_stat_database_blks_hit{datname="lab"}[5m]) + rate(pg_stat_database_blks_read{datname="lab"}[5m]))` | should sit ~0.99 |

**9. `docs/08-infrastructure.md`** started — the exporter pattern, plus the 8a
walkthrough (drive load, watch pool utilization rise, drop `SetMaxOpenConns` to
2 and watch waiters appear).

### Definition of Done — 8a

- [ ] Fresh `make up` boots Postgres, runs the schema, app connects and serves
      `/users` `/products` `/orders` from the DB
- [ ] `db_query_duration_seconds` and `db_pool_*` appear on `/metrics`; `query`
      label is a bounded set of call-site names
- [ ] Prometheus `postgres` target is UP; `pg_up == 1`
- [ ] Under `make load`, pool-in-use rises; setting `SetMaxOpenConns(2)` makes
      `db_pool_wait_count_total` climb and query p95 follow
- [ ] `store_test.go` runs against a throwaway DB, **skips cleanly** when
      `API_DATABASE_URL` is unset (so `make test` stays hermetic)
- [ ] "Postgres" dashboard row populated
- [ ] Tagged `phase-8a`

---

## Sub-phase 8b — Redis

Add a read-through cache for `GET /products`, measure hit ratio, add
`redis_exporter`.

### What to build

**1. `internal/cache/cache.go`** — `github.com/redis/go-redis/v9`, one method
pair:

```go
func (c *Cache) Products(ctx context.Context, load func(context.Context) ([]Product, error)) ([]Product, error) {
    if b, err := c.rdb.Get(ctx, "products").Bytes(); err == nil {
        c.metrics.CacheResult("hit")
        return decode(b)
    }
    c.metrics.CacheResult("miss")
    v, err := load(ctx)
    if err != nil { return nil, err }
    c.rdb.Set(ctx, "products", encode(v), 30*time.Second)
    return v, nil
}
```

A Redis error is a **miss**, not a request failure — the cache is an
optimization, `load()` is the source of truth. Log it, count it
(`cache_errors_total`), fall through.

**2. `cache_hits_total{result}`** — counter, `result="hit"|"miss"`. Hit ratio in
PromQL: `rate(cache_hits_total{result="hit"}[5m]) / rate(cache_hits_total[5m])`.

**3. Wire it.** `handleProducts` becomes
`cache.Products(r.Context(), store.Products)`. `handleCreateOrder` stays
DB-direct (writes don't cache). Optionally bust `products` on any future product
write — out of scope now, note it.

**4. Config.** `RedisAddr string` (`API_REDIS_ADDR`, default `redis:6379`).
Redis unreachable at startup → log a warning and run **cache-disabled** (every
call is a miss straight to the DB). Unlike Postgres, the app still works without
Redis — model that.

**5. `docker-compose.yml`**:

```yaml
  redis:
    image: redis:7.4-alpine
    command: ["redis-server", "--save", "", "--maxmemory", "64mb", "--maxmemory-policy", "allkeys-lru"]

  redis-exporter:
    image: oliver006/redis_exporter:v1.67.0
    environment:
      REDIS_ADDR: "redis://redis:6379"
```

**6. Scrape job**:

```yaml
  - job_name: redis
    static_configs:
      - targets: ["redis-exporter:9121"]
```

**7. Dashboard — "Redis" row on `infra.json`**:

| Panel | Query |
|-------|-------|
| Hit ratio (app-side) | `rate(cache_hits_total{result="hit"}[$__rate_interval]) / rate(cache_hits_total[$__rate_interval])` |
| Ops/s | `rate(redis_commands_processed_total[$__rate_interval])` |
| Redis keyspace hit ratio | `rate(redis_keyspace_hits_total[5m]) / (rate(redis_keyspace_hits_total[5m]) + rate(redis_keyspace_misses_total[5m]))` |
| Memory used | `redis_memory_used_bytes` vs `redis_memory_max_bytes` |
| Connected clients | `redis_connected_clients` |
| Evicted keys/s | `rate(redis_evicted_keys_total[$__rate_interval])` — non-zero once `maxmemory` bites |

**8.** `docs/08-infrastructure.md` — 8b walkthrough: cold start → first request
is a miss → next 30s are hits → TTL expiry → miss again. Then `redis-cli
FLUSHALL` mid-load and watch the hit ratio notch down and DB query rate spike.

### Definition of Done — 8b

- [ ] `GET /products` served from cache after the first call; `cache_hits_total`
      splits hit/miss
- [ ] Stopping `redis` → app keeps serving `/products` (all misses), logs the
      degradation, `cache_errors_total` climbs
- [ ] `redis` target UP; `redis_up == 1`
- [ ] "Redis" dashboard row populated; hit ratio visibly rises after warm-up
- [ ] `cache_test.go` covers hit, miss, and Redis-down → fallthrough; skips when
      `API_REDIS_ADDR` unset
- [ ] Tagged `phase-8b`

---

## Sub-phase 8c — Kafka

Publish an event per order, consume it in a separate binary, and make
**consumer lag** a first-class panel.

### What to build

**1. `internal/events/producer.go`** — `github.com/segmentio/kafka-go`. On a
successful `CreateOrder`, publish `{order_id, product_id, qty, ts}` to topic
`orders`. Fire-and-forget with a bounded async writer; a publish failure
increments `orders_publish_errors_total` and is **not** a request failure (the
order is already committed to Postgres — the DB is the source of truth, Kafka is
a downstream notification).

**2. `cmd/consumer/main.go`** — third binary (the image already builds
`./cmd/...`, same as `/sink`). Joins consumer group `order-processors`, reads
`orders`, does token work (`time.Sleep(consumerDelayMs)` — a knob), logs one
line per event. Exposes its own `/metrics` on `:9100`... **no** — reuse `:9000`
range: listen `:9002`. Metrics: `orders_consumed_total`,
`order_processing_duration_seconds`.

**3. Metrics.**

| Metric | Where | Meaning |
|--------|-------|---------|
| `orders_published_total` | app | events written to Kafka |
| `orders_publish_errors_total` | app | publish failures (order still succeeded) |
| `orders_consumed_total` | consumer | events processed |
| `kafka_consumergroup_lag` | exporter | the number that matters |

**4. Config.** App: `KafkaBrokers string` (`API_KAFKA_BROKERS`, default
`kafka:9092`), Kafka unreachable → publish disabled + warn (order path still
works). Consumer: `CONSUMER_KAFKA_BROKERS`, `CONSUMER_DELAY_MS` (default `50`).

**5. `docker-compose.yml`** — single-node Kafka in KRaft mode (no ZooKeeper):

```yaml
  kafka:
    image: apache/kafka:3.9.0
    environment:
      KAFKA_NODE_ID: 1
      KAFKA_PROCESS_ROLES: broker,controller
      KAFKA_LISTENERS: "PLAINTEXT://:9092,CONTROLLER://:9093"
      KAFKA_ADVERTISED_LISTENERS: "PLAINTEXT://kafka:9092"
      KAFKA_CONTROLLER_QUORUM_VOTERS: "1@kafka:9093"
      KAFKA_CONTROLLER_LISTENER_NAMES: CONTROLLER
      KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: 1
      KAFKA_AUTO_CREATE_TOPICS_ENABLE: "true"

  consumer:
    build: .
    entrypoint: ["/consumer"]
    depends_on: [kafka]

  kafka-exporter:
    image: danielqsj/kafka-exporter:v1.8.0
    command: ["--kafka.server=kafka:9092", "--group.filter=order-processors"]
    depends_on: [kafka]
```

**6. Scrape jobs** — `kafka-exporter:9308`, and the consumer's own
`:9002`:

```yaml
  - job_name: kafka
    static_configs:
      - targets: ["kafka-exporter:9308"]
  - job_name: consumer
    static_configs:
      - targets: ["consumer:9002"]
```

**7. Dashboard — "Kafka" row on `infra.json`**:

| Panel | Query |
|-------|-------|
| Consumer group lag | `sum(kafka_consumergroup_lag{consumergroup="order-processors"})` |
| Publish rate vs consume rate | `rate(orders_published_total[$__rate_interval])` vs `rate(orders_consumed_total[$__rate_interval])` |
| Processing p95 | `histogram_quantile(0.95, sum by (le) (rate(order_processing_duration_seconds_bucket[$__rate_interval])))` |
| Publish errors/s | `rate(orders_publish_errors_total[$__rate_interval])` |

**8. Recording + alert rule** (ties back to Phase 7): add to
`prometheus/rules/alerts.yml`:

```yaml
  - alert: OrderConsumerLagging
    expr: sum(kafka_consumergroup_lag{consumergroup="order-processors"}) > 500
    for: 3m
    labels: { severity: warning }
    annotations:
      summary: "Order consumer group lag over 500"
      description: "Lag is {{ $value }} for 3m — the async order pipeline is falling behind."
```

**9.** `docs/08-infrastructure.md` — 8c walkthrough: baseline lag ~0; bump
`CONSUMER_DELAY_MS=400` and run `make spike`; watch produce-rate outrun
consume-rate, lag climb, `OrderConsumerLagging` fire and reach the webhook sink;
drop the delay and watch lag drain back to 0.

### Definition of Done — 8c

- [ ] Every `POST /orders` increments `orders_published_total`; `consumer` logs
      the event and increments `orders_consumed_total`
- [ ] `kafka` + `consumer` targets UP; `kafka_consumergroup_lag` present
- [ ] `CONSUMER_DELAY_MS` high + spike → lag rises, then drains when lowered
- [ ] `OrderConsumerLagging` fires and lands in the webhook sink
- [ ] Stopping `kafka` → orders still return 201, `orders_publish_errors_total`
      climbs, no 5xx
- [ ] "Kafka" dashboard row populated
- [ ] Tagged `phase-8c`

---

## Sub-phase 8d — Host / container metrics

No app changes — pure exporter wiring for the layer under everything.

### What to build

**1. `docker-compose.yml`**:

```yaml
  node-exporter:
    image: prom/node-exporter:v1.8.2
    command:
      - "--path.rootfs=/host"
    pid: host
    volumes:
      - "/:/host:ro,rslave"

  cadvisor:
    image: gcr.io/cadvisor/cadvisor:v0.49.1
    privileged: true
    devices: ["/dev/kmsg"]
    volumes:
      - "/:/rootfs:ro"
      - "/var/run:/var/run:ro"
      - "/sys:/sys:ro"
      - "/var/lib/docker/:/var/lib/docker:ro"
```

**2. Scrape jobs**:

```yaml
  - job_name: node
    static_configs:
      - targets: ["node-exporter:9100"]
  - job_name: cadvisor
    static_configs:
      - targets: ["cadvisor:8080"]
```

Add `metric_relabel_configs` on the `cadvisor` job to **drop** the per-container
series you'll never chart (filesystem, tmpfs, network per-interface) — this is
the cardinality-hygiene exercise:

```yaml
    metric_relabel_configs:
      - source_labels: [__name__]
        regex: "container_(network_tcp_usage_total|tasks_state|fs_.*|blkio_.*)"
        action: drop
      - source_labels: [container_label_com_docker_compose_service]
        regex: "^$"
        action: drop   # drop the machine-level and pause-container noise
```

**3. Dashboard — "Host / Containers" row on `infra.json`**:

| Panel | Query |
|-------|-------|
| Host CPU busy % | `100 - (avg(rate(node_cpu_seconds_total{mode="idle"}[$__rate_interval])) * 100)` |
| Host mem used % | `(1 - node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes) * 100` |
| Host disk free % (data volume) | `node_filesystem_avail_bytes{mountpoint="/host"} / node_filesystem_size_bytes{mountpoint="/host"} * 100` |
| Per-container memory | `sum by (container_label_com_docker_compose_service) (container_memory_working_set_bytes{container_label_com_docker_compose_service!=""})` |
| Per-container CPU | `sum by (container_label_com_docker_compose_service) (rate(container_cpu_usage_seconds_total{container_label_com_docker_compose_service!=""}[$__rate_interval]))` |

**4. Alert rule** — the "you have hours to fix this" class:

```yaml
  - alert: HostDiskFillingUp
    expr: predict_linear(node_filesystem_avail_bytes{mountpoint="/host"}[1h], 4*3600) < 0
    for: 10m
    labels: { severity: warning }
    annotations:
      summary: "Host data disk projected to fill within 4h"
```

**5.** `docs/08-infrastructure.md` — finish it: the USE method table for host
resources, and a note on what these numbers mean *on Docker Desktop* (see
Traps).

### Definition of Done — 8d

- [ ] `node` + `cadvisor` targets UP
- [ ] "Host / Containers" row shows per-service memory and CPU for every
      container in the stack, keyed by compose service name
- [ ] cadvisor cardinality trimmed via `metric_relabel_configs`; verify with
      `count({job="cadvisor"})` before/after
- [ ] `HostDiskFillingUp` rule loads and is `inactive` (don't force it to fire)
- [ ] `docs/08-infrastructure.md` complete
- [ ] Tagged `phase-8d`

---

## Definition of Done — whole phase

```sh
make check-config            # rules still valid with the new infra alerts
make up                      # ~12 containers; give it 30-60s to settle
make load
open http://localhost:9090/targets    # api, alertmanager, postgres, redis, kafka, consumer, node, cadvisor all UP
make dash-infra                        # the new infra dashboard
```

- [ ] All four sub-phases' DoDs met and tagged (`phase-8a`..`phase-8d`)
- [ ] `docker compose ps` — every exporter and backing service healthy
- [ ] `/targets` shows 8 jobs UP
- [ ] `make test` still passes with **no** running infra (integration tests skip)
- [ ] `infra.json` provisioned automatically, 4 rows, all panels populated under load
- [ ] Phase 7's `webhook-sink` receives `OrderConsumerLagging` when the consumer
      is throttled
- [ ] `docs/08-infrastructure.md` written; README checklist P8 ticked
- [ ] `make check` clean (fmt, vet, test)
- [ ] Committed, PR opened

## Traps to notice

- **`make test` must not need Docker.** Every `*_test.go` that touches Postgres /
  Redis / Kafka guards on its env var and `t.Skip()`s when unset. A hermetic
  unit suite is non-negotiable; integration coverage is opt-in.
- **`app` starting before Postgres is ready** — without
  `depends_on: {condition: service_healthy}` the app crashes on first boot, then
  Compose restarts it and it looks fine, hiding a real ordering bug. Use the
  healthcheck.
- **cadvisor on Docker Desktop (macOS/Windows)** is flaky and privileged — it
  reads the Linux VM, not your Mac. `node-exporter` likewise reports the VM's
  CPU/mem/disk, not the host's. That's fine for the lab (the *pattern* is the
  lesson) but say so in the doc so nobody debugs a "wrong" disk number. If
  cadvisor won't start, `container_*` metrics also come from Prometheus's own
  scrape of the Docker daemon in some setups — don't rabbit-hole, note it and
  move on.
- **`query` / `topic` / `container` labels blowing up cardinality** — the
  app-side `query` label is a fixed enum of call-site names, never SQL. The
  cadvisor job *must* have `metric_relabel_configs` or Prometheus's series count
  jumps ~10x from one exporter.
- **Kafka KRaft single-node** needs `KAFKA_OFFSETS_TOPIC_REPLICATION_FACTOR: 1`
  (default 3) or the `__consumer_offsets` topic never creates and the consumer
  group silently never commits — lag reads as either zero or garbage.
- **Redis error treated as a request error** — a cache is an optimization. Redis
  down must degrade to "slow but working" (all misses), never to 5xx. Same
  principle as Kafka: the DB is the source of truth, everything else is best
  effort.
- **`repeat_interval` / rule-group churn** — you're adding alerts to the Phase 7
  files; re-run `make check-config` and confirm `/rules` shows the new group
  healthy, not just that the file parses.
- **Exporter version skew** — pin every exporter image tag. `redis_exporter` and
  `postgres_exporter` rename metrics between minor versions; an unpinned `latest`
  will silently break a dashboard panel months later.
