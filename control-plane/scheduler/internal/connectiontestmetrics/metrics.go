// Package connectiontestmetrics registers Prometheus descriptors for the
// Connection Test Executor. The executor records samples after authoritative
// completion. Names and labels follow docs/observability/metrics-catalog.md.
//
// Label normalization rules live in
// io.astrasync/control-plane/observability/normalize (see ADR-058 §3 +
// ADR-061 §2). Every Recorder method that derives a label value from
// caller input MUST route through that package; package-level CounterVec
// access is reserved for tests and the registration boundary.
package connectiontestmetrics

import (
	"errors"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"io.astrasync/control-plane/observability/normalize"
)

const (
	// OutcomeSuccess identifies a connection test that completed successfully.
	OutcomeSuccess = "success"
	// OutcomeFailure identifies a connection test that completed with an internal or remote failure.
	OutcomeFailure = "failure"
	// OutcomeRejected identifies a connection test rejected by the egress policy.
	OutcomeRejected = "rejected"
)

// outcomeAllowlist is the canonical outcome allowlist for
// connection_test_total. The slice is the single source of truth
// used by Observe (via normalize.NormalizeOutcome) and is the
// documented contract for any new Recorder owner that wants to
// reuse the connection-test outcome values (ADR-061 §2).
//
// Any value outside the allowlist collapses to OutcomeFailure,
// matching the catalog row "outcome is success, rejected, or
// failure" and the legacy `switch outcome` behavior recorded in
// the pre-slice-45 Observe contract.
var outcomeAllowlist = []string{OutcomeSuccess, OutcomeRejected, OutcomeFailure}

// ConnectionTestTotal counts connection test outcomes. Registered
// against the global default registry so existing scrapes (the
// legacy Handler() entry point) continue to expose the series
// without a code change. New call sites should use Recorder.
var ConnectionTestTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "connection_test_total",
	Help: "Total connection tests executed by the Connection Test Executor.",
}, []string{"tenant_id", "outcome"})

// Recorder owns the connection-test metric family used by one
// executor. The Recorder routes every label value that derives
// from caller input through io.astrasync/control-plane/observability/normalize
// so the connection-test Recorder contract is identical to every
// other tenant-deriving Recorder in the control plane (ADR-058 §3,
// ADR-061 §2).
type Recorder struct {
	ConnectionTestTotal *prometheus.CounterVec
}

// NewRecorder registers an isolated connection-test recorder with
// the supplied registerer. The supplied registerer must not have
// registered the same metric name already (prometheus returns
// "already registered" on collision).
//
// The Recorder's *Vec reference is independent from the
// package-level ConnectionTestTotal so a long-running consumer
// (Connection Test Executor daemon) can host the Recorder without
// competing with the process-global scrape surface that
// Handler() continues to expose.
func NewRecorder(registerer prometheus.Registerer) (*Recorder, error) {
	if registerer == nil {
		return nil, ErrNilRegisterer
	}
	recorder := &Recorder{
		ConnectionTestTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "connection_test_total",
			Help: "Total connection tests executed by the Connection Test Executor.",
		}, []string{"tenant_id", "outcome"}),
	}
	if err := registerer.Register(recorder.ConnectionTestTotal); err != nil {
		return nil, ErrDuplicateMetric.Wrap(err)
	}
	return recorder, nil
}

// DefaultRecorder returns a recorder backed by the process-global
// metric family exposed by Handler. New call sites should prefer
// NewRecorder(registerer) so the Recorder-owned registry can be
// hosted from a long-running consumer's own /metrics endpoint;
// DefaultRecorder remains for backward compatibility with the
// pre-slice-45 import path.
func DefaultRecorder() *Recorder {
	return &Recorder{ConnectionTestTotal: ConnectionTestTotal}
}

// Observe records one authoritative connection-test completion
// with bounded tenant and outcome labels. tenant_id routes
// through normalize.NormalizeTenant; outcome routes through
// normalize.NormalizeOutcome with the documented
// `success | rejected | failure` allowlist and OutcomeFailure as
// the default value for non-allowlisted inputs. Calls with a
// nil receiver are silently dropped so callers can pass a
// Recorder only at the boundary sites that own it.
func (r *Recorder) Observe(tenantID, outcome string) {
	if r == nil || r.ConnectionTestTotal == nil {
		return
	}
	r.ConnectionTestTotal.WithLabelValues(
		normalize.NormalizeTenant(tenantID),
		normalize.NormalizeOutcome(outcome, outcomeAllowlist, OutcomeFailure),
	).Inc()
}

// Handler returns the Prometheus HTTP handler that scrapes the global
// default registerer (the same surface that ConnectionTestTotal
// is auto-registered against). The slice-45 design keeps this
// entry point stable so any existing import (including the legacy
// metrics_test.go) continues to scrape the same series.
func Handler() http.Handler {
	return promhttp.Handler()
}

// HandlerFor returns a Prometheus HTTP handler that scrapes the
// supplied gatherer. Long-running consumers (Connection Test
// Executor daemon) host a Recorder through their own registerer
// and expose the Recorder-owned metric through this handler
// instead of through Handler().
func HandlerFor(gatherer prometheus.Gatherer) http.Handler {
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{EnableOpenMetrics: true})
}

// ErrNilRegisterer is returned by NewRecorder when the caller
// passes a nil prometheus.Registerer. The error exposes a
// sentinel value so callers can detect the misuse without
// string-matching on a wrapped prometheus error.
var ErrNilRegisterer = &RecorderError{reason: "metrics registerer must not be nil"}

// ErrDuplicateMetric is returned by NewRecorder when the
// supplied registerer already registered a metric with the same
// name (prometheus reports "duplicate metrics collector
// registration attempted"). The error exposes a sentinel value
// so callers can detect the collision without string-matching
// on the wrapped prometheus message.
var ErrDuplicateMetric = &RecorderError{reason: "register connection test metric"}

// RecorderError reports an error raised while constructing or
// using a Recorder. The cause (if any) wraps the underlying
// prometheus error.
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
