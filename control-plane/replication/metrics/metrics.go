// Package metrics exposes bounded Prometheus metrics for multi-region operations.
//
// Label normalization rules live in
// io.astrasync/control-plane/observability/normalize (see ADR-058 §3 +
// ADR-063 §2). Every Recorder method that derives a label value from
// caller input MUST route through that package: target_region / peer_region
// through NormalizeFreeText; event_type / outcome through NormalizeOutcome
// with the documented allowlists.
package metrics

import (
	"errors"
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"io.astrasync/control-plane/observability/normalize"
)

// freeTextMaxLen bounds the target_region / peer_region label
// cardinality. The Prometheus client cap is not enforced; we bound
// here so a runaway caller cannot emit arbitrary-length series.
// Mirrors observability/normalize.workerIDMaxLen (128 bytes).
const freeTextMaxLen = 128

// freeTextUnknown is the canonical empty-value sentinel for free-form
// region labels (target_region / peer_region). Matches the catalog row
// for astrasync_multi_region_*_total: "an unset target region is
// _unknown."
const freeTextUnknown = "_unknown"

// Outcome labels for promotion / event / recovery families. Matches
// catalog row: "outcome is success or failure."
const (
	OutcomeSuccess = "success"
	OutcomeFailure = "failure"
)

// promotionOutcomeAllowlist is the canonical outcome allowlist for
// astrasync_multi_region_promotion_total. Matches the catalog row
// "outcome is success or failure." Used by ObservePromotion via
// normalize.NormalizeOutcome.
//
// Per-family allowlists (rather than a single shared slice) preserve
// the per-family contract: a future ADR can extend one family's
// outcome allowlist without silently extending the others.
var promotionOutcomeAllowlist = []string{OutcomeSuccess, OutcomeFailure}

// eventOutcomeAllowlist is the canonical outcome allowlist for
// astrasync_multi_region_event_total. Matches the catalog row
// "outcome is success or failure."
var eventOutcomeAllowlist = []string{OutcomeSuccess, OutcomeFailure}

// recoveryOutcomeAllowlist is the canonical outcome allowlist for
// astrasync_multi_region_recovery_total. Matches the catalog row
// "outcome is success or failure."
var recoveryOutcomeAllowlist = []string{OutcomeSuccess, OutcomeFailure}

// eventTypeAllowlist is the canonical event_type allowlist for
// astrasync_multi_region_event_total. Matches the catalog row
// "event_type is checkpoint, topology, or health." Non-allowlisted
// values collapse to freeTextUnknown ("_unknown") rather than to
// OutcomeFailure, because event_type is a categorical tag, not an
// outcome: a stray event_type value should be flagged as "unknown
// event category", not mislabelled as a failure.
var eventTypeAllowlist = []string{"checkpoint", "topology", "health"}

// Recorder records multi-region promotion metrics.
type Recorder struct {
	PromotionsTotal    *prometheus.CounterVec
	PromotionsDuration *prometheus.HistogramVec
	EventsTotal        *prometheus.CounterVec
	EventsDuration     *prometheus.HistogramVec
	RecoveriesTotal    *prometheus.CounterVec
	RecoveriesDuration *prometheus.HistogramVec
}

// Bundle owns the registry and recorder shared by replication components.
type Bundle struct {
	Registry *prometheus.Registry
	Recorder *Recorder
}

// NewBundle creates an isolated registry and recorder for one process.
func NewBundle() (*Bundle, error) {
	registry := prometheus.NewRegistry()
	recorder, err := NewRecorder(registry)
	if err != nil {
		return nil, err
	}
	return &Bundle{Registry: registry, Recorder: recorder}, nil
}

// NewRecorder registers an isolated multi-region metrics recorder.
//
// The supplied registerer must not have registered the same metric
// name already (prometheus returns "already registered" on
// collision). Sentinel errors are exposed via ErrNilRegisterer and
// ErrDuplicateMetric so callers can detect misconfiguration without
// string-matching on the wrapped prometheus error.
func NewRecorder(registerer prometheus.Registerer) (*Recorder, error) {
	if registerer == nil {
		return nil, ErrNilRegisterer
	}
	recorder := &Recorder{
		PromotionsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "astrasync_multi_region_promotion_total",
			Help: "Total multi-region promotion attempts by target region and outcome.",
		}, []string{"target_region", "outcome"}),
		PromotionsDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "astrasync_multi_region_promotion_duration_seconds",
			Help:    "Duration of multi-region promotion attempts.",
			Buckets: prometheus.DefBuckets,
		}, []string{"target_region"}),
		EventsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "astrasync_multi_region_event_total",
			Help: "Total cross-region events by peer region, type, and outcome.",
		}, []string{"peer_region", "event_type", "outcome"}),
		EventsDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "astrasync_multi_region_event_duration_seconds",
			Help:    "Duration of cross-region event delivery attempts.",
			Buckets: prometheus.DefBuckets,
		}, []string{"peer_region", "event_type"}),
		RecoveriesTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "astrasync_multi_region_recovery_total",
			Help: "Total cross-region checkpoint recovery attempts by target region and outcome.",
		}, []string{"target_region", "outcome"}),
		RecoveriesDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "astrasync_multi_region_recovery_duration_seconds",
			Help:    "Duration of cross-region checkpoint recovery attempts.",
			Buckets: prometheus.DefBuckets,
		}, []string{"target_region"}),
	}
	for _, collector := range []prometheus.Collector{
		recorder.PromotionsTotal, recorder.PromotionsDuration,
		recorder.EventsTotal, recorder.EventsDuration,
		recorder.RecoveriesTotal, recorder.RecoveriesDuration,
	} {
		if err := registerer.Register(collector); err != nil {
			return nil, ErrDuplicateMetric.Wrap(err)
		}
	}
	return recorder, nil
}

// ObservePromotion records one completed promotion attempt. target_region
// routes through normalize.NormalizeFreeText; outcome routes through
// normalize.NormalizeOutcome with promotionOutcomeAllowlist. nil
// receivers are silently dropped so callers can pass a Recorder only
// at the boundary sites that own it.
func (r *Recorder) ObservePromotion(targetRegion, outcome string, duration time.Duration) {
	if r == nil {
		return
	}
	targetRegion = normalize.NormalizeFreeText(targetRegion, freeTextMaxLen, freeTextUnknown)
	outcome = normalize.NormalizeOutcome(outcome, promotionOutcomeAllowlist, OutcomeFailure)
	r.PromotionsTotal.WithLabelValues(targetRegion, outcome).Inc()
	r.PromotionsDuration.WithLabelValues(targetRegion).Observe(normalizeDuration(duration).Seconds())
}

// ObserveEvent records one cross-region event delivery attempt.
// peer_region routes through normalize.NormalizeFreeText; event_type
// routes through normalize.NormalizeOutcome with eventTypeAllowlist
// (fallback = freeTextUnknown); outcome routes through
// normalize.NormalizeOutcome with eventOutcomeAllowlist. nil
// receivers are silently dropped.
func (r *Recorder) ObserveEvent(peerRegion, eventType, outcome string, duration time.Duration) {
	if r == nil {
		return
	}
	peerRegion = normalize.NormalizeFreeText(peerRegion, freeTextMaxLen, freeTextUnknown)
	eventType = normalize.NormalizeOutcome(eventType, eventTypeAllowlist, freeTextUnknown)
	outcome = normalize.NormalizeOutcome(outcome, eventOutcomeAllowlist, OutcomeFailure)
	r.EventsTotal.WithLabelValues(peerRegion, eventType, outcome).Inc()
	r.EventsDuration.WithLabelValues(peerRegion, eventType).Observe(normalizeDuration(duration).Seconds())
}

// ObserveRecovery records one checkpoint recovery attempt.
// target_region routes through normalize.NormalizeFreeText; outcome
// routes through normalize.NormalizeOutcome with
// recoveryOutcomeAllowlist. nil receivers are silently dropped.
func (r *Recorder) ObserveRecovery(targetRegion, outcome string, duration time.Duration) {
	if r == nil {
		return
	}
	targetRegion = normalize.NormalizeFreeText(targetRegion, freeTextMaxLen, freeTextUnknown)
	outcome = normalize.NormalizeOutcome(outcome, recoveryOutcomeAllowlist, OutcomeFailure)
	r.RecoveriesTotal.WithLabelValues(targetRegion, outcome).Inc()
	r.RecoveriesDuration.WithLabelValues(targetRegion).Observe(normalizeDuration(duration).Seconds())
}

func normalizeDuration(duration time.Duration) time.Duration {
	if duration < 0 {
		return 0
	}
	return duration
}

// Handler returns an OpenMetrics-capable handler for the global registry.
func Handler() http.Handler {
	return HandlerFor(prometheus.DefaultGatherer)
}

// HandlerFor returns an OpenMetrics-capable handler for the supplied gatherer.
func HandlerFor(gatherer prometheus.Gatherer) http.Handler {
	if gatherer == nil {
		return http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			http.Error(response, "metrics gatherer must not be nil", http.StatusInternalServerError)
		})
	}
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

// ErrNilRegisterer is returned by NewRecorder when the caller passes a
// nil prometheus.Registerer. Exposed as a sentinel so callers can
// detect misconfiguration via errors.Is without string-matching.
var ErrNilRegisterer = &RecorderError{reason: "metrics registerer must not be nil"}

// ErrDuplicateMetric is returned by NewRecorder when the supplied
// registerer has already registered a metric with the same name.
// Exposed as a sentinel so callers can detect the collision via
// errors.Is without string-matching on the wrapped prometheus error.
var ErrDuplicateMetric = &RecorderError{reason: "register multi-region metric"}

// RecorderError reports an error raised while constructing a Recorder.
// The cause (if any) wraps the underlying prometheus error.
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
// ErrDuplicateMetric sentinel. Mirrors slice-44.1 / slice-45.1
// RecorderError design.
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

// Wrap turns a raw prometheus error into a RecorderError so callers can
// build a single sentinel for "duplicate metric" without importing
// prometheus.
func (e *RecorderError) Wrap(cause error) *RecorderError {
	return &RecorderError{reason: e.reason, cause: cause}
}
