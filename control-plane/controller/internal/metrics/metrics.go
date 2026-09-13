// Package metrics registers Prometheus descriptors for Controller business
// metrics and provides the recorder used by reconcile call sites.
//
// Label normalization rules live in io.astrasync/control-plane/observability/normalize
// (see ADR-058 §3). Every Recorder method that derives a label value from
// caller input MUST route through that package; package-level CounterVec
// access is restricted to tests and the registration boundary.
package metrics

import (
	"fmt"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"io.astrasync/control-plane/observability/normalize"
)

// ControllerJobStateTotal counts one Controller-driven Job state transition.
// Labels follow docs/observability/metrics-catalog.md: `tenant_id`,
// `namespace`, `from_state`, `to_state`. The Recorder-owned allowlist for
// state values is recorded here so future transition sources do not have
// to rediscover the contract (ADR-058 §2 + ADR-029 durable state machine).
var ControllerJobStateTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "controller_job_state_total",
	Help: "Controller-driven SyncJob state transitions committed to durable state.",
}, []string{"tenant_id", "namespace", "from_state", "to_state"})

// ControllerEpochFenceTotal counts fence attempts that the Controller
// records when a Scheduler report indicates an obsolete execution epoch
// (ADR-030 lease-fenced dispatch). The Recorder uses `success|fenced|failure`
// as the outcome allowlist (ADR-058 §3) — `fenced` is exclusive to this
// metric and records a clean fence response (the lease-fenced writer
// was successfully fenced off).
var ControllerEpochFenceTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	Name: "controller_epoch_fence_total",
	Help: "Epoch-fence attempts recorded by the Controller reconcile boundary.",
}, []string{"tenant_id", "outcome"})

// epochFenceOutcomeAllowlist enumerates the outcome label values accepted
// for controller_epoch_fence_total. The list mirrors ADR-058 §3; values
// outside the list collapse to "failure" through normalize.NormalizeOutcome.
var epochFenceOutcomeAllowlist = []string{"success", "fenced", "failure"}

// stateValueMaxLen bounds `from_state` and `to_state` label values. The
// controller Job state machine enumerates fixed values — `created`,
// `compiling`, `validating`, `ready`, `submitting`, `running`,
// `checkpointing`, `failing`, `paused`, `completed`, `failed` — each well
// below this cap. The cap exists so a stale reconcile state (one not yet
// listed above) collapses to `_unknown` rather than widening the series
// count to a free-form string.
const stateValueMaxLen = 32

// Recorder records Controller reconcile metrics.
//
// The Recorder owns *Vec references for the families it knows how to
// update. Methods that derive label values from caller input route
// through io.astrasync/control-plane/observability/normalize so every
// Recorder-emitting path enforces identical label-allowlist rules
// (ADR-058 §3).
type Recorder struct {
	ReconcileDuration *prometheus.HistogramVec
	JobStateTotal     *prometheus.CounterVec
	EpochFenceTotal   *prometheus.CounterVec
}

// NewRecorder registers an isolated Controller metrics recorder with the
// supplied registerer. Controller-runtime owns the registry used in production.
func NewRecorder(registerer prometheus.Registerer) (*Recorder, error) {
	if registerer == nil {
		return nil, fmt.Errorf("metrics registerer must not be nil")
	}
	recorder := &Recorder{
		ReconcileDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "controller_job_controller_reconcile_duration_seconds",
			Help:    "Latency of Controller SyncJob reconcile iterations.",
			Buckets: prometheus.DefBuckets,
		}, []string{"tenant_id", "outcome"}),
		JobStateTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "controller_job_state_total",
			Help: "Controller-driven SyncJob state transitions committed to durable state.",
		}, []string{"tenant_id", "namespace", "from_state", "to_state"}),
		EpochFenceTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "controller_epoch_fence_total",
			Help: "Epoch-fence attempts recorded by the Controller reconcile boundary.",
		}, []string{"tenant_id", "outcome"}),
	}
	for _, collector := range []prometheus.Collector{
		recorder.ReconcileDuration, recorder.JobStateTotal, recorder.EpochFenceTotal,
	} {
		if err := registerer.Register(collector); err != nil {
			return nil, fmt.Errorf("register Controller metric: %w", err)
		}
	}
	return recorder, nil
}

// ObserveReconcile records one completed Controller reconcile iteration.
// tenantID and outcome routes through normalize so the slice-43.3 label
// contract is identical to every other tenant-deriving Recorder in the
// control plane (ADR-058 §3).
func (r *Recorder) ObserveReconcile(tenantID, outcome string, duration time.Duration) {
	if r == nil || r.ReconcileDuration == nil {
		return
	}
	if duration < 0 {
		duration = 0
	}
	r.ReconcileDuration.WithLabelValues(
		normalize.NormalizeTenant(tenantID),
		normalize.NormalizeOutcome(outcome, []string{"success", "failure"}, "failure"),
	).Observe(duration.Seconds())
}

// ObserveStateTransition records one durable Job state transition. Every
// label value passes through normalize so a stale reconcile state (one
// not in the documented Job state machine) collapses to `_unknown`
// rather than widening the series count. Calls with a nil receiver are
// silently dropped so callers can pass a Recorder only at the boundary
// sites that own it.
func (r *Recorder) ObserveStateTransition(tenantID, namespace, fromState, toState string) {
	if r == nil || r.JobStateTotal == nil {
		return
	}
	r.JobStateTotal.WithLabelValues(
		normalize.NormalizeTenant(tenantID),
		normalize.NormalizeWorkerID(namespace),
		normalizeStateValue(fromState),
		normalizeStateValue(toState),
	).Inc()
}

// ObserveEpochFence records one epoch-fence attempt at the Controller
// reconcile boundary. The outcome allowlist is
// `success|fenced|failure` (ADR-058 §3); `fenced` records a successful
// fence of an obsolete writer, distinct from `success` which records an
// epoch-equality reconciliation.
func (r *Recorder) ObserveEpochFence(tenantID, outcome string) {
	if r == nil || r.EpochFenceTotal == nil {
		return
	}
	r.EpochFenceTotal.WithLabelValues(
		normalize.NormalizeTenant(tenantID),
		normalize.NormalizeOutcome(outcome, epochFenceOutcomeAllowlist, "failure"),
	).Inc()
}

// normalizeStateValue bounds `from_state` / `to_state` label values to
// the fixed Job state machine. The cap mirrors scheduler's worker-id
// length rule (slice 43.0) so future state names cannot accidentally
// widen the cardinality budget for either label.
func normalizeStateValue(value string) string {
	if value == "" || len(value) > stateValueMaxLen {
		return "_unknown"
	}
	return value
}
