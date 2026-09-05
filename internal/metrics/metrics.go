package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// Metrics owns every metric this service exposes and the registry they are
// served from. Keep it instance-scoped so tests get fresh collectors.
type Metrics struct {
	registry *prometheus.Registry

	requestsTotal          *prometheus.CounterVec
	requestDuration        *prometheus.HistogramVec
	requestDurationSummary *prometheus.SummaryVec
	requestsInProgress     *prometheus.GaugeVec
	ordersCreated          prometheus.Counter
}

func New() *Metrics {
	reg := prometheus.NewRegistry()
	reg.MustRegister(collectors.NewGoCollector())
	reg.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	m := &Metrics{
		registry: reg,
		requestsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests, by method, route, and status.",
		}, []string{"method", "route", "status"}),
		requestDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration in seconds, by method and route.",
			Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2, 3},
		}, []string{"method", "route"}),
		requestDurationSummary: prometheus.NewSummaryVec(prometheus.SummaryOpts{
			Name: "http_request_duration_summary_seconds",
			Help: "Teaching contrast only: the same observations as the histogram, " +
				"as a client-side Summary that cannot be aggregated across instances.",
			Objectives: map[float64]float64{0.5: 0.05, 0.95: 0.01, 0.99: 0.001},
		}, []string{"method", "route"}),
		requestsInProgress: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "http_requests_in_progress",
			Help: "In-flight HTTP requests, by route.",
		}, []string{"route"}),
		ordersCreated: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "orders_created_total",
			Help: "Total orders successfully created.",
		}),
	}

	reg.MustRegister(
		m.requestsTotal,
		m.requestDuration,
		m.requestDurationSummary,
		m.requestsInProgress,
		m.ordersCreated,
	)
	return m
}

// Registry returns the registry the router mounts /metrics against.
func (m *Metrics) Registry() *prometheus.Registry {
	return m.registry
}

// OrderCreated records a successfully created order. Called from the orders
// handler itself, not from the RED middleware — this is a business metric,
// not an HTTP one.
func (m *Metrics) OrderCreated() {
	m.ordersCreated.Inc()
}
