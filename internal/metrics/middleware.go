package metrics

import (
	"net/http"
	"strconv"
	"time"
)

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(code int) {
	if !r.wroteHeader {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

func (m *Metrics) Instrument(routeFunc func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			route := routeFunc(r)
			m.requestsInProgress.WithLabelValues(route).Inc()
			defer m.requestsInProgress.WithLabelValues(route).Dec()

			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
			start := time.Now()

			defer func() {
				status := rec.status
				rp := recover()
				if rp != nil {
					status = http.StatusInternalServerError
				}

				elapsed := time.Since(start).Seconds()
				m.requestsTotal.WithLabelValues(r.Method, route, strconv.Itoa(status)).Inc()
				m.requestDuration.WithLabelValues(r.Method, route).Observe(elapsed)
				m.requestDurationSummary.WithLabelValues(r.Method, route).Observe(elapsed)

				if rp != nil {
					panic(rp)
				}
			}()

			next.ServeHTTP(rec, r)
		})
	}
}
