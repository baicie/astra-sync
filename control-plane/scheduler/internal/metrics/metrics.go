// Package metrics registers Prometheus descriptors for Scheduler business
// metrics and provides the recorder used by reconcile + dispatch call
// sites. The names and labels follow the convention documented at
// docs/observability/metrics-catalog.md.
//
// Label normalization rules live in
// io.astrasync/control-plane/control-plane/observability/normalize (see
// ADR-058 §3). Every Recorder method that derives a label value from
// caller input MUST route through that package; package-level
// CounterVec / HistogramVec access is reserved for tests and the
// registration boundary.
package metrics

import (
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"io.astrasync/control-plane/observability/normalize"
)

// JobAssignmentTotal counts the splits the scheduler hands to workers.
// Registered against the global default registry so existing scrapes
// (the legacy Handler() entry point) continue to expose the series
// without a code change. New call sites should use Recorder.
var JobAssignmentTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "scheduler_job_assignment_total",
	Help: "Total job splits assigned by the scheduler.",
}, []string{"tenant_id", "worker_id", "outcome"})

// LeaseTakeoverTotal counts leadership lease takeovers. Registered
// against the global default registry so existing scrapes continue to
// work; new call sites should use Recorder.
var LeaseTakeoverTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "scheduler_lease_takeover_total",
	Help: "Total leadership lease takeovers performed by the scheduler.",
}, []string{"tenant_id", "outcome"})

// JobReconcileDuration records the latency of one job reconcile
// iteration. Registered against the global default registry so existing
// scrapes continue to work; new call sites should use Recorder.
var JobReconcileDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "scheduler_job_reconcile_duration_seconds",
	Help:    "Latency of job reconcile iterations.",
	Buckets: prometheus.DefBuckets,
}, []string{"tenant_id"})

// assignmentOutcomeAllowlist enumerates the outcome label values
// accepted for scheduler_job_assignment_total. The list mirrors
// docs/observability/metrics-catalog.md (allowlist: success |
// rejected | failure); values outside the list collapse to "failure"
// through normalize.NormalizeOutcome.
var assignmentOutcomeAllowlist = []string{"success", "rejected", "failure"}

// leaseTakeoverOutcomeAllowlist enumerates the outcome label values
// accepted for scheduler_lease_takeover_total. The catalog records
// success as the only documented outcome (ADR-058 §2 +
// docs/observability/metrics-catalog.md); the allowlist is
// intentionally tighter than the auth outcome allowlist so any
// non-success emission collapses to "_unknown" rather than
// "failure".
var leaseTakeoverOutcomeAllowlist = []string{"success", "_unknown"}

// Recorder records Scheduler business metrics through an injected
// Prometheus registerer so a long-running consumer (Scheduler
// daemon) can expose the families from its own /metrics endpoint
// without creating a second listener or competing for the global
// default registry.
//
// Recorder owns *Vec references for the families it knows how to
// update. Methods that derive label values from caller input route
// through io.astrasync/control-plane/observability/normalize so
// every Recorder-emitting path enforces identical label-allowlist
// rules (ADR-058 §3, ADR-060 §3).
type Recorder struct {
	AssignmentTotal    *prometheus.CounterVec
	LeaseTakeoverTotal *prometheus.CounterVec
	ReconcileDuration  *prometheus.HistogramVec
}

// NewRecorder registers an isolated Scheduler metrics recorder with
// the supplied registerer. The supplied registerer must not have
// registered the same metric name already (prometheus returns
// "already registered" on collision).
//
// The Recorder's *Vec references are independent from the
// package-level JobAssignmentTotal / LeaseTakeoverTotal /
// JobReconcileDuration so a long-running consumer can host the
// Recorder without competing with the process-global scrape surface
// that Handler() continues to expose.
func NewRecorder(registerer prometheus.Registerer) (*Recorder, error) {
	if registerer == nil {
		return nil, ErrNilRegisterer
	}
	recorder := &Recorder{
		AssignmentTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "scheduler_job_assignment_total",
			Help: "Total job splits assigned by the scheduler.",
		}, []string{"tenant_id", "worker_id", "outcome"}),
		LeaseTakeoverTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "scheduler_lease_takeover_total",
			Help: "Total leadership lease takeovers performed by the scheduler.",
		}, []string{"tenant_id", "outcome"}),
		ReconcileDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "scheduler_job_reconcile_duration_seconds",
			Help:    "Latency of job reconcile iterations.",
			Buckets: prometheus.DefBuckets,
		}, []string{"tenant_id"}),
	}
	for _, collector := range []prometheus.Collector{
		recorder.AssignmentTotal,
		recorder.LeaseTakeoverTotal,
		recorder.ReconcileDuration,
	} {
		if err := registerer.Register(collector); err != nil {
			return nil, ErrDuplicateMetric.Wrap(err)
		}
	}
	return recorder, nil
}

// ErrNilRegisterer is returned by NewRecorder when the caller passes a
// nil prometheus.Registerer. The error exposes a sentinel value so
// callers can detect the misuse without string-matching on a wrapped
// prometheus error.
var ErrNilRegisterer = &RecorderError{reason: "metrics registerer must not be nil"}

// ErrDuplicateMetric is returned by NewRecorder when the supplied
// registerer already registered a metric with the same name
// (prometheus reports "duplicate metrics collector registration
// attempted"). The error exposes a sentinel value so callers can
// detect the collision without string-matching on the wrapped
// prometheus message.
var ErrDuplicateMetric = &RecorderError{reason: "register scheduler metric"}

// RecorderError reports an error raised while constructing or using a
// Recorder. The cause (if any) wraps the underlying prometheus error.
type RecorderError struct {
	reason string
	cause  error
}

func (e *RecorderError) Error() string {
	if e == nil {
		return "<nil>"
	}
	if e.cause == nil {
		return e.reason
	}
	return e.reason + ": " + e.cause.Error()
}

func (e *RecorderError) Unwrap() error { return e.cause }

// Is allows callers to compare RecorderError values with errors.Is.
// The comparison walks the Unwrap chain so that a RecorderError
// wrapping a prometheus.AlreadyRegisteredError still matches the
// ErrDuplicateMetric sentinel (the underlying error's message is
// "duplicate metrics collector registration attempted"; we do not
// assert on the message because prometheus has not committed to it).
func (e *RecorderError) Is(target error) bool {
	if e == nil || target == nil {
		return false
	}
	if e.reason == ErrDuplicateMetric.reason && target == ErrDuplicateMetric {
		return true
	}
	if e.reason == ErrNilRegisterer.reason && target == ErrNilRegisterer {
		return true
	}
	return errors.Is(e.cause, target)
}

// Wrap turns a raw prometheus error into a RecorderError so callers
// can build a single sentinel for "duplicate metric" without
// importing prometheus.
func (e *RecorderError) Wrap(cause error) *RecorderError {
	return &RecorderError{reason: e.reason, cause: cause}
}

// ObserveAssignment records one Scheduler assignment outcome. The
// tenant_id, worker_id, and outcome labels all route through
// normalize so the slice-44 contract is identical to every other
// tenant-deriving Recorder in the control plane (ADR-058 §3).
// requestID is recorded verbatim — the Scheduler dispatch surface
// does not yet have a documented allowlist for request IDs, so the
// recorder does not normalize. Calls with a nil receiver are
// silently dropped so callers can pass a Recorder only at the
// boundary sites that own it.
func (r *Recorder) ObserveAssignment(tenantID, workerID, outcome, requestID string) {
	if r == nil || r.AssignmentTotal == nil {
		return
	}
	r.AssignmentTotal.WithLabelValues(
		normalize.NormalizeTenant(tenantID),
		normalize.NormalizeWorkerID(workerID),
		normalize.NormalizeOutcome(outcome, assignmentOutcomeAllowlist, "failure"),
	).Inc()
}

// ObserveLeaseTakeover records one Scheduler lease takeover outcome.
// tenant_id routes through normalize; outcome routes through
// normalize with the documented allowlist `success` (non-success
// values collapse to `_unknown` per the catalog contract). Calls
// with a nil receiver are silently dropped.
func (r *Recorder) ObserveLeaseTakeover(tenantID, outcome, requestID string) {
	if r == nil || r.LeaseTakeoverTotal == nil {
		return
	}
	r.LeaseTakeoverTotal.WithLabelValues(
		normalize.NormalizeTenant(tenantID),
		normalize.NormalizeOutcome(outcome, leaseTakeoverOutcomeAllowlist, "_unknown"),
	).Inc()
}

// ObserveReconcile records one Scheduler reconcile iteration. The
// histogram observes the supplied duration verbatim (the duration
// is a sample, not a label); tenant_id routes through normalize.
// Calls with a nil receiver are silently dropped.
func (r *Recorder) ObserveReconcile(tenantID string, duration time.Duration) {
	if r == nil || r.ReconcileDuration == nil {
		return
	}
	if duration < 0 {
		duration = 0
	}
	r.ReconcileDuration.WithLabelValues(
		normalize.NormalizeTenant(tenantID),
	).Observe(duration.Seconds())
}

// Handler returns the Prometheus HTTP handler that scrapes the global
// default registerer (the same surface that JobAssignmentTotal /
// LeaseTakeoverTotal / JobReconcileDuration are auto-registered
// against). The slice-44 design keeps this entry point stable so any
// existing import (including the legacy metrics_test.go) continues to
// scrape the same series.
func Handler() http.Handler {
	return promhttp.Handler()
}

// HandlerFor returns a Prometheus HTTP handler that scrapes the
// supplied gatherer. Long-running consumers (Scheduler daemon) host
// a Recorder through their own registerer and expose the
// Recorder-owned metrics through this handler instead of through
// Handler().
func HandlerFor(gatherer prometheus.Gatherer) http.Handler {
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{EnableOpenMetrics: true})
}
