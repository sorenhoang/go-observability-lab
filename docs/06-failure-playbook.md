# Phase 6 Failure Playbook

Phase 6 adds a small control plane for creating known failures on demand. The
goal is not just to break the service; it is to connect each failure mode to the
RED dashboard and the runtime panels so the signal becomes predictable.

If `API_ADMIN_TOKEN` is unset, the control endpoints are open and the API logs a
startup warning. If it is set, pass `Authorization: Bearer <token>` or
`?token=<token>` with every control request.

## Fault Table

| Fault | Trigger | Panels that move | Reading | Screenshot |
| --- | --- | --- | --- | --- |
| Injected latency | `curl -s -XPOST localhost:8080/admin/chaos -d '{"enabled":true,"latency_ms":400}'` | Latency percentiles up, latency heatmap shifts into higher buckets, in-flight may rise. Rate and Error % stay roughly flat. | Everything is slower, but not failing. Think slow dependency, lock contention, queueing, or an overloaded shared resource before thinking crash. | Add screenshot here. |
| Injected errors | `curl -s -XPOST localhost:8080/admin/chaos -d '{"enabled":true,"error_ratio":0.3}'` | Error % rises, 5xx by route rises across business routes. Duration may stay flat. | The service is returning failures across the data plane. Because all business routes move, it is service-wide rather than one bad endpoint. | Add screenshot here. |
| CPU burn | `curl -s "localhost:8080/cpu?seconds=20"` | CPU cores used rises toward `GOMAXPROCS`, then latency and in-flight can rise if the scheduler is starved. | CPU saturation is the cause; slow requests are the symptom. Look at runtime panels before chasing downstream latency. | Add screenshot here. |
| Memory leak | `curl -s "localhost:8080/leak?mb=150"; curl -s "localhost:8080/leak?mb=150"` | Heap in use steps up and stays high. GC time /s may rise. | A leak has a shape: memory does not return to baseline after traffic settles. Use `curl -s -XPOST localhost:8080/leak/reset` to drop the held allocations and force a GC. | Add screenshot here. |
| Traffic spike | `make spike` | Rate ramps from baseline to the spike, in-flight rises, latency can rise under contention, then all recover when traffic returns to baseline. | This is load, not necessarily a bug. The recover stage should bring Rate, in-flight, and latency back down. | Add screenshot here. |

## Reset Commands

Disable injected latency/errors:

```sh
curl -s -XPOST localhost:8080/admin/chaos -d '{"enabled":false}'
```

Drop leaked memory:

```sh
curl -s -XPOST localhost:8080/leak/reset
```

## Dashboard Notes

The RED panels show request symptoms: rate, errors, duration, and in-flight
work. The Runtime / Saturation row shows process causes: goroutines, heap, GC
time, CPU cores used, and whether chaos injection is enabled.

The `chaos_enabled` annotation marks the moment runtime injection flips. It is a
marker for injected latency and injected errors, not for `/cpu`, `/leak`, or
`make spike`, which are direct control-plane actions.
