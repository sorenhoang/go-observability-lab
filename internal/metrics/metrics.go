package metrics

import (
	"database/sql"
	"time"

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
	ordersPublished        prometheus.Counter
	orderPublishErrors     prometheus.Counter
	dbQueryDuration        *prometheus.HistogramVec
	cacheHits              *prometheus.CounterVec
	cacheErrors            prometheus.Counter
	chaosEnabled           prometheus.Gauge
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
		ordersPublished: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "orders_published_total",
			Help: "Total order events successfully published to Kafka.",
		}),
		orderPublishErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "orders_publish_errors_total",
			Help: "Total order event publish failures.",
		}),
		dbQueryDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "db_query_duration_seconds",
			Help:    "Database query duration in seconds, by fixed query name and status.",
			Buckets: []float64{.001, .0025, .005, .01, .025, .05, .1, .25, .5, 1},
		}, []string{"query", "status"}),
		cacheHits: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "cache_hits_total",
			Help: "Read-through cache results by hit or miss.",
		}, []string{"result"}),
		cacheErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "cache_errors_total",
			Help: "Total cache operation errors. Cache errors degrade to source-of-truth reads.",
		}),
		chaosEnabled: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "chaos_enabled",
			Help: "Whether runtime chaos injection is enabled.",
		}),
	}

	reg.MustRegister(
		m.requestsTotal,
		m.requestDuration,
		m.requestDurationSummary,
		m.requestsInProgress,
		m.ordersCreated,
		m.ordersPublished,
		m.orderPublishErrors,
		m.dbQueryDuration,
		m.cacheHits,
		m.cacheErrors,
		m.chaosEnabled,
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

func (m *Metrics) OrderPublished() {
	m.ordersPublished.Inc()
}

func (m *Metrics) OrderPublishError() {
	m.orderPublishErrors.Inc()
}

func (m *Metrics) ObserveDBQuery(query, status string, d time.Duration) {
	m.dbQueryDuration.WithLabelValues(query, status).Observe(d.Seconds())
}

func (m *Metrics) RegisterDBStats(db *sql.DB) {
	m.registry.MustRegister(newDBStatsCollector(db))
}

func (m *Metrics) CacheResult(result string) {
	m.cacheHits.WithLabelValues(result).Inc()
}

func (m *Metrics) CacheError() {
	m.cacheErrors.Inc()
}

// ChaosEnabled records whether runtime fault injection is enabled.
func (m *Metrics) ChaosEnabled(enabled bool) {
	if enabled {
		m.chaosEnabled.Set(1)
		return
	}
	m.chaosEnabled.Set(0)
}

type dbStatsCollector struct {
	db                   *sql.DB
	openConnectionsDesc  *prometheus.Desc
	inUseConnectionsDesc *prometheus.Desc
	idleConnectionsDesc  *prometheus.Desc
	waitCountDesc        *prometheus.Desc
	waitDurationDesc     *prometheus.Desc
}

func newDBStatsCollector(db *sql.DB) *dbStatsCollector {
	return &dbStatsCollector{
		db: db,
		openConnectionsDesc: prometheus.NewDesc(
			"db_pool_open_connections",
			"Current number of established database connections.",
			nil,
			nil,
		),
		inUseConnectionsDesc: prometheus.NewDesc(
			"db_pool_in_use_connections",
			"Current number of database connections in use.",
			nil,
			nil,
		),
		idleConnectionsDesc: prometheus.NewDesc(
			"db_pool_idle_connections",
			"Current number of idle database connections.",
			nil,
			nil,
		),
		waitCountDesc: prometheus.NewDesc(
			"db_pool_wait_count_total",
			"Total number of database connection waits.",
			nil,
			nil,
		),
		waitDurationDesc: prometheus.NewDesc(
			"db_pool_wait_duration_seconds_total",
			"Total duration blocked waiting for a database connection.",
			nil,
			nil,
		),
	}
}

func (c *dbStatsCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.openConnectionsDesc
	ch <- c.inUseConnectionsDesc
	ch <- c.idleConnectionsDesc
	ch <- c.waitCountDesc
	ch <- c.waitDurationDesc
}

func (c *dbStatsCollector) Collect(ch chan<- prometheus.Metric) {
	stats := c.db.Stats()
	ch <- prometheus.MustNewConstMetric(c.openConnectionsDesc, prometheus.GaugeValue, float64(stats.OpenConnections))
	ch <- prometheus.MustNewConstMetric(c.inUseConnectionsDesc, prometheus.GaugeValue, float64(stats.InUse))
	ch <- prometheus.MustNewConstMetric(c.idleConnectionsDesc, prometheus.GaugeValue, float64(stats.Idle))
	ch <- prometheus.MustNewConstMetric(c.waitCountDesc, prometheus.CounterValue, float64(stats.WaitCount))
	ch <- prometheus.MustNewConstMetric(c.waitDurationDesc, prometheus.CounterValue, stats.WaitDuration.Seconds())
}
