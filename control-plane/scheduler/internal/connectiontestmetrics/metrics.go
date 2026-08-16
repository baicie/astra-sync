// Package connectiontestmetrics registers the connection-test-executor
// Prometheus metrics. The names and labels follow the convention
// documented at docs/observability/metrics-catalog.md.
package connectiontestmetrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// ConnectionTestTotal counts connection test outcomes.
var ConnectionTestTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "connection_test_executor_connection_test_total",
	Help: "Total connection tests executed by the Connection Test Executor.",
}, []string{"tenant_id", "outcome"})

// Handler returns the Prometheus HTTP handler that scrapes the global
// default registerer.
func Handler() http.Handler {
	return promhttp.Handler()
}
