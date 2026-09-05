# API Reference

Phase 1 keeps the service deliberately small: six routes, in-memory data, and
two fault simulators for later observability work.

| Path | Method | Behavior | Tunable |
| --- | --- | --- | --- |
| `/health` | `GET` | Returns service status and uptime. | None |
| `/users` | `GET` | Returns three fake users. | None |
| `/products` | `GET` | Returns three fake products. | None |
| `/orders` | `POST` | Creates an in-memory order from `product_id` and positive `qty`; returns `201`, `400`, or `404`. | None |
| `/slow` | `GET` | Sleeps for a random duration in `[min, max]` ms, then returns `{"slept_ms":N}`. Returns immediately (no body) if the client disconnects. | `API_SLOW_MIN_MS`, `API_SLOW_MAX_MS`, `?max_ms=` |
| `/error` | `GET` | Returns `500 {"error":"simulated failure"}` with the configured probability; otherwise `200`. | `API_ERROR_RATE`, `?rate=` |

`/slow` and `/error` are production-problem simulators. Later phases use them
to create known latency and failure events, then correlate those faults with
what Prometheus metrics and Grafana dashboards show.

## Tunable ranges

Both the env defaults and the per-request query overrides are range-checked so a
typo can't put the API into a nonsensical state:

- `API_ERROR_RATE` / `?rate=` — clamped to `[0, 1]` (an out-of-range query value
  is ignored and the configured default is used).
- `API_SLOW_MIN_MS` / `API_SLOW_MAX_MS` / `?max_ms=` — negatives become `0`; if
  `min > max`, `min` is lowered to `max`.
