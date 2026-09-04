# 00 — Why observability

## The problem

A service is running in production. A user says "it's slow". You have SSH access
and `top`. Now what?

Without instrumentation you are guessing. You cannot answer:

- How many requests per second is it handling right now, and is that normal?
- Which endpoint is slow — all of them, or just one?
- Is it slow for everyone, or only the 95th-percentile unlucky request?
- Did this start 5 minutes ago or has it been degrading for a day?
- Is it CPU, memory, a downstream dependency, or lock contention?

**Observability** is the property of a system that lets you answer questions like
these *from the outside*, without shipping new code to add a `printf`.

## The three pillars

| Pillar | What it is | Answers |
|--------|------------|---------|
| **Metrics** | Numeric time series, cheap to store, aggregatable | "How much / how many / how fast" — trends, rates, percentiles, alerts |
| **Logs** | Timestamped events, often with structure | "What exactly happened at 14:32:07 for request X" |
| **Traces** | The path of one request across services, with timing per hop | "Where did those 800 ms go — which service, which call" |

They are complementary. Metrics tell you *something is wrong and roughly where*;
traces and logs tell you *exactly what*.

## This lab is metrics-only

On purpose. Prometheus and Grafana are the metrics tools, and metrics are where
most people start and spend most of their operational time. Traces (Tempo /
Jaeger) and logs (Loki) are a separate lab.

Practical consequence: when a dashboard here shows "P95 latency on `/orders`
spiked", you will *correlate it with what you did* (you toggled chaos), not
click through to a trace. That correlation muscle is the point.

## The RED method

The dashboard philosophy for this lab. For any request-driven service, watch
three things:

- **R**ate — requests per second
- **E**rrors — failed requests per second (or as a ratio)
- **D**uration — latency distribution (P50 / P95 / P99), not the average

RED is the "if you only have one dashboard" dashboard. (Its sibling, the USE
method — Utilization, Saturation, Errors — is for resources like CPU, disks,
pools; you will meet it in Phase 6 and Phase 8.)

## The pull model

Prometheus **scrapes**. Your app exposes a plain-text `/metrics` endpoint;
Prometheus fetches it on an interval (every 5 s in this lab) and stores what it
sees. Your app does not push anywhere and does not know Prometheus exists beyond
serving that one endpoint. Phase 3 is where this clicks.
