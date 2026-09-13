package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/trace"
)

// exemplarFor is the only place the sampled-check lives — every histogram
// observe (HTTP duration, DB query duration) calls this instead of deciding
// on its own whether to attach an exemplar. An exemplar for an unsampled or
// absent span would link to a trace Tempo never actually recorded — an
// unresolvable link is worse than no link, so this returns nil rather than
// guess.
func exemplarFor(ctx context.Context) prometheus.Labels {
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() || !sc.IsSampled() {
		return nil
	}
	return prometheus.Labels{"trace_id": sc.TraceID().String()}
}

// observeWithExemplar records v on obs, attaching a trace_id exemplar when
// ctx carries a sampled span. Both call sites that observe a histogram
// (Instrument's HTTP duration, ObserveDBQuery) go through this so the
// sampled-check in exemplarFor is never duplicated.
func observeWithExemplar(obs prometheus.Observer, ctx context.Context, v float64) {
	if l := exemplarFor(ctx); l != nil {
		obs.(prometheus.ExemplarObserver).ObserveWithExemplar(v, l)
		return
	}
	obs.Observe(v)
}
