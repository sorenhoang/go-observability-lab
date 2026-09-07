# Infrastructure Observability

Phase 8 adds the layer below the Go API: Postgres, Redis, Kafka, host metrics,
and container metrics. The key pattern is the exporter: a sidecar process reads a
system's native stats and republishes them as Prometheus metrics.

App-side metrics and exporter metrics answer different questions. App-side
metrics show how the dependency feels to users, for example
`db_query_duration_seconds` or `cache_hits_total`. Exporter metrics show what the
dependency says about itself, for example `pg_stat_database_*`,
`redis_keyspace_*`, `kafka_consumergroup_lag`, `node_*`, and `container_*`.

## Postgres

The API now reads `/users`, `/products`, and `/orders` from Postgres through
`database/sql` with the pgx stdlib driver. The pool is intentionally small:
`MaxOpenConns=10`, `MaxIdleConns=5`.

Useful checks:

```sh
make up
curl localhost:8080/users
curl localhost:8080/products
curl -XPOST localhost:8080/orders -d '{"product_id":1,"qty":1}'
curl -s localhost:8080/metrics | grep 'db_query_duration_seconds\|db_pool_'
```

In Grafana, open `make dash-infra` and use the Postgres row:

- Query p95 shows the app's query latency by fixed query name.
- Pool in use/open shows utilization against the small pool limit.
- Pool waiters shows saturation. Non-zero waits mean callers are queued for a DB
  connection.
- Commits, rollbacks, and buffer hit ratio come from `postgres_exporter`.

To make saturation obvious, temporarily lower `SetMaxOpenConns` to `2`, run
`make load`, and watch `db_pool_wait_count_total` and query p95 move together.

## Redis

`GET /products` uses a read-through cache with a 30 second TTL. Redis is an
optimization only: when Redis is down, the API records a miss/error and falls
through to Postgres. It should not return a 5xx unless Postgres itself fails.

Useful checks:

```sh
curl localhost:8080/products
curl localhost:8080/products
curl -s localhost:8080/metrics | grep 'cache_hits_total\|cache_errors_total'
docker compose stop redis
curl localhost:8080/products
docker compose start redis
```

The first request after a cold cache is a miss. Requests inside the TTL are hits.
After `docker compose exec redis redis-cli FLUSHALL`, the hit ratio should notch
down and DB query activity should rise.

## Kafka

Successful orders are committed to Postgres first, then the API publishes an
order event to Kafka on a best-effort path. Kafka publish failures increment
`orders_publish_errors_total` but do not change the HTTP response because
Postgres is the source of truth.

The `consumer` binary joins group `order-processors`, reads the `orders` topic,
does a small configurable sleep, and exposes metrics on `:9002`.

Useful checks:

```sh
curl -XPOST localhost:8080/orders -d '{"product_id":1,"qty":1}'
curl -s localhost:8080/metrics | grep orders_published_total
docker compose logs consumer
```

Consumer lag is the headline Kafka signal:

```promql
sum(kafka_consumergroup_lag{consumergroup="order-processors"})
```

To exercise lag and the Phase 7 alert path, run the stack with a high
`CONSUMER_DELAY_MS`, then run `make spike`. `OrderConsumerLagging` fires when lag
stays above 500 for 3 minutes and Alertmanager sends it to the webhook sink.

## Host And Containers

`node-exporter` reports CPU, memory, filesystem, and kernel-level metrics.
`cadvisor` reports per-container CPU and memory. The cadvisor scrape includes
`metric_relabel_configs` that drop unused high-cardinality filesystem, blkio,
task-state, and unlabeled container series.

Useful checks:

```promql
count({job="cadvisor"})
sum by (container_label_com_docker_compose_service) (
  container_memory_working_set_bytes{container_label_com_docker_compose_service!=""}
)
```

USE view for this lab:

| Resource | Utilization | Saturation | Errors |
|---|---|---|---|
| DB pool | `db_pool_in_use_connections / 10` | `rate(db_pool_wait_count_total[5m])` | query status label |
| CPU | non-idle `node_cpu_seconds_total` | sustained 100% busy | usually app-level symptoms |
| Memory | used / total bytes | swap or OOM behavior | container restarts |
| Disk | used / total bytes | projected time to full | filesystem errors |

`HostDiskFillingUp` uses:

```promql
predict_linear(node_filesystem_avail_bytes{mountpoint="/host"}[1h], 4*3600) < 0
```

On Docker Desktop for macOS or Windows, node-exporter and cadvisor report the
Linux VM that runs Docker containers, not the physical host OS. This compose file
mounts `/` without Linux bind propagation flags because Docker Desktop can reject
`rslave` with "not a shared or slave mount". That is expected for this lab; the
learning objective is the observability pattern.

## Known gaps

These shipped as-is (Phase 8, PR #9) and are left in as teaching artefacts. See
[`docs/phases/phase-8.md`](phases/phase-8.md#known-issues) for the full write-up.

1. **Kafka publishing is off after a cold `make up`.** The producer does a
   one-shot startup dial with no retry and compose only waits for the Kafka
   *process*, not a ready broker. `docker compose restart app` once the stack is
   up, or fix the producer to lean on `kafka-go`'s built-in reconnect.
2. **`HostDiskFillingUp` and the disk panel are empty.** `--path.rootfs=/host`
   makes node-exporter strip the `/host` prefix from `mountpoint` labels, so
   `node_filesystem_avail_bytes{mountpoint="/host"}` never matches. Point the
   rule and panel at `mountpoint="/"` or `mountpoint="/var/lib/docker"`.
3. **A Redis blip at startup disables the cache for the process lifetime.** The
   per-request path already falls through to Postgres correctly; the startup Ping
   should be advisory, not a one-way switch.
