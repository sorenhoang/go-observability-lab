// Package obs holds cross-cutting observability plumbing shared by the API, the
// consumer, and the sink: a context-aware structured logger, request middleware
// (panic guard, canonical request logging, HTTP trace spans), OpenTelemetry
// tracer wiring, and hand-rolled W3C trace-context propagation for Kafka
// message headers.
//
// Import rule (enforced to avoid a cycle): obs may import only the standard
// library and go.opentelemetry.io/otel*. It must never import internal/metrics
// or internal/api — internal/metrics imports obs for exemplars in Phase 12.
package obs
