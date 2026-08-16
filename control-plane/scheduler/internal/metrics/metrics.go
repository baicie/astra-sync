// Package metrics registers Prometheus descriptors for Scheduler business
// metrics and provides its /metrics handler. Business call sites create the
// samples. The names and labels follow the convention documented at
// docs/observability/metrics-catalog.md.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// JobAssignmentTotal counts the splits the scheduler hands to workers.
var JobAssignmentTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "scheduler_job_assignment_total",
	Help: "Total job splits assigned by the scheduler.",
}, []string{"tenant_id", "worker_id", "outcome"})

// LeaseTakeoverTotal counts leadership lease takeovers.
var LeaseTakeoverTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "scheduler_lease_takeover_total",
	Help: "Total leadership lease takeovers performed by the scheduler.",
}, []string{"tenant_id", "outcome"})

// JobReconcileDuration records the latency of one job reconcile iteration.
var JobReconcileDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "scheduler_job_reconcile_duration_seconds",
	Help:    "Latency of job reconcile iterations.",
	Buckets: prometheus.DefBuckets,
}, []string{"tenant_id"})

// Handler returns the Prometheus HTTP handler that scrapes the global
// default registerer.
func Handler() http.Handler {
	return promhttp.Handler()
}
