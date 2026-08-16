// Package metrics registers the business Prometheus metrics that the
// AstraSync Console exposes on its /metrics listener. The names and
// labels follow the convention documented at
// docs/observability/metrics-catalog.md.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ConsoleRequestTotal counts request outcomes served by the Console.
var ConsoleRequestTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "console_request_total",
	Help: "Total requests served by the Console.",
}, []string{"tenant_id", "outcome", "handler"})

// ConsoleRenderDuration records the latency of Console rendering.
var ConsoleRenderDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "console_render_duration_seconds",
	Help:    "Latency of Console HTML rendering.",
	Buckets: prometheus.DefBuckets,
}, []string{"handler"})

// Handler returns the Prometheus HTTP handler that scrapes the global
// default registerer.
func Handler() http.Handler {
	return promhttp.Handler()
}
