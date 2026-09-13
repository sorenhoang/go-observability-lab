#!/usr/bin/env bash
# Simulates a latency incident and prints where to watch it land, pillar by
# pillar. Walks: RED dashboard P95 spikes -> hover an exemplar dot -> "View
# trace" -> the trace shows the latency in the chaos middleware span ->
# "Logs for this span" -> the WARN "chaos injected failure" line, same
# trace_id. See docs/12-correlation.md for the full walkthrough.
set -euo pipefail

APP_HOST="${APP_HOST:-localhost:8080}"
GRAFANA_HOST="${GRAFANA_HOST:-localhost:3000}"
LATENCY_MS="${LATENCY_MS:-800}"

echo "==> enabling chaos: latency_ms=${LATENCY_MS} on ${APP_HOST}"
curl -sf -X POST "http://${APP_HOST}/admin/chaos" \
  -H 'Content-Type: application/json' \
  -d "{\"enabled\":true,\"latency_ms\":${LATENCY_MS}}" >/dev/null

echo "==> chaos enabled; run 'make load' in another shell if it isn't already"
echo "==> waiting 20s for a few scrapes to pick up the spike"
sleep 20

echo
echo "==> RED dashboard (watch P95 climb, hover a dot on the p95 line for an exemplar):"
echo "    http://${GRAFANA_HOST}/d/red/red-dashboard"
echo
echo "==> next: click the exemplar -> 'View trace' -> find the chaos-affected span"
echo "    -> 'Logs for this span' -> confirm the WARN line shares the trace's trace_id"
echo
echo "==> disable chaos when done:"
echo "    curl -X POST http://${APP_HOST}/admin/chaos -d '{\"enabled\":false}'"
