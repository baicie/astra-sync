// Package connectiontestmetrics registers Prometheus descriptors for the
// Connection Test Executor. The executor records samples after authoritative
// completion. Names and labels follow docs/observability/metrics-catalog.md.
package connectiontestmetrics

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	// OutcomeSuccess identifies a connection test that completed successfully.
	OutcomeSuccess = "success"
	// OutcomeFailure identifies a connection test that completed with an internal or remote failure.
	OutcomeFailure = "failure"
	// OutcomeRejected identifies a connection test rejected by the egress policy.
	OutcomeRejected = "rejected"
)

// ConnectionTestTotal counts connection test outcomes.
var ConnectionTestTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "connection_test_total",
	Help: "Total connection tests executed by the Connection Test Executor.",
}, []string{"tenant_id", "outcome"})

// Recorder owns the connection-test metric families used by one executor.
type Recorder struct {
	ConnectionTestTotal *prometheus.CounterVec
}

// NewRecorder registers an isolated connection-test recorder.
func NewRecorder(registerer prometheus.Registerer) (*Recorder, error) {
	if registerer == nil {
		return nil, fmt.Errorf("metrics registerer must not be nil")
	}
	recorder := &Recorder{
		ConnectionTestTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "connection_test_total",
			Help: "Total connection tests executed by the Connection Test Executor.",
		}, []string{"tenant_id", "outcome"}),
	}
	if err := registerer.Register(recorder.ConnectionTestTotal); err != nil {
		return nil, fmt.Errorf("register connection test metric: %w", err)
	}
	return recorder, nil
}

// DefaultRecorder returns a recorder backed by the process-global metric
// family exposed by Handler.
func DefaultRecorder() *Recorder {
	return &Recorder{ConnectionTestTotal: ConnectionTestTotal}
}

// Observe records one authoritative connection-test completion with bounded
// tenant and outcome labels.
func (r *Recorder) Observe(tenantID, outcome string) {
	if r == nil || r.ConnectionTestTotal == nil {
		return
	}
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = "_unknown"
	}
	switch outcome {
	case OutcomeSuccess, OutcomeRejected, OutcomeFailure:
	default:
		outcome = OutcomeFailure
	}
	r.ConnectionTestTotal.WithLabelValues(tenantID, outcome).Inc()
}

// Handler returns the Prometheus HTTP handler that scrapes the global
// default registerer.
func Handler() http.Handler {
	return promhttp.Handler()
}
