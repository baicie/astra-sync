// Package metrics registers Prometheus descriptors for Controller business
// metrics and provides the recorder used by reconcile call sites.
package metrics

import (
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
)

const unknownTenant = "_unknown"

// Recorder records Controller reconcile metrics.
type Recorder struct {
	ReconcileDuration *prometheus.HistogramVec
}

// NewRecorder registers an isolated Controller metrics recorder with the
// supplied registerer. Controller-runtime owns the registry used in production.
func NewRecorder(registerer prometheus.Registerer) (*Recorder, error) {
	if registerer == nil {
		return nil, fmt.Errorf("metrics registerer must not be nil")
	}
	recorder := &Recorder{ReconcileDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "controller_job_controller_reconcile_duration_seconds",
		Help:    "Latency of Controller SyncJob reconcile iterations.",
		Buckets: prometheus.DefBuckets,
	}, []string{"tenant_id", "outcome"})}
	if err := registerer.Register(recorder.ReconcileDuration); err != nil {
		return nil, fmt.Errorf("register Controller metric: %w", err)
	}
	return recorder, nil
}

// ObserveReconcile records one completed Controller reconcile iteration.
func (r *Recorder) ObserveReconcile(tenantID, outcome string, duration time.Duration) {
	if r == nil || r.ReconcileDuration == nil {
		return
	}
	if duration < 0 {
		duration = 0
	}
	r.ReconcileDuration.WithLabelValues(normalizeTenant(tenantID), normalizeOutcome(outcome)).Observe(duration.Seconds())
}

func normalizeTenant(value string) string {
	parsed, err := uuid.Parse(value)
	if err != nil || parsed.String() != value {
		return unknownTenant
	}
	return value
}

func normalizeOutcome(value string) string {
	switch value {
	case "success", "failure":
		return value
	default:
		return "failure"
	}
}
