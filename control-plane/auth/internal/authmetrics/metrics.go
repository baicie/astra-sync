// Package authmetrics registers Prometheus descriptors for the auth library.
// Future server-side auth call sites can import the package and create samples
// through the Recorder; the one-shot admin CLI neither imports this package
// nor binds a /metrics port, so the Recorder-owned CounterVec samples only
// land on a registerer that a long-running consumer (API Server, Console
// forwarder) explicitly attaches.
//
// Label normalization rules live in
// io.astrasync/control-plane/control-plane/observability/normalize (see
// ADR-058 §3). Every Recorder method that derives a label value from
// caller input MUST route through that package; package-level CounterVec
// access is reserved for tests and the registration boundary.
package authmetrics

import (
	"errors"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"io.astrasync/control-plane/observability/normalize"
)

// AuthSignInTotal counts sign-in outcomes served by the auth library.
// Registered against the process-global default registry so existing
// scrapes (the legacy `Handler()` entry point) continue to expose the
// series without a code change. New call sites should use Recorder.
var AuthSignInTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "auth_sign_in_total",
	Help: "Total sign-in flows served by the auth library.",
}, []string{"tenant_id", "outcome"})

// AuthSessionRevokeTotal counts session revocations served by the auth
// library. Registered against the process-global default registry so
// existing scrapes continue to work; new call sites should use Recorder.
var AuthSessionRevokeTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "auth_session_revoke_total",
	Help: "Total session revocations served by the auth library.",
}, []string{"tenant_id"})

// authOutcomeAllowlist enumerates the outcome label values accepted
// for auth_sign_in_total. The list mirrors
// docs/observability/metrics-catalog.md (allowlist: success | rejected
// | failure); values outside the list collapse to "failure" through
// normalize.NormalizeOutcome.
var authOutcomeAllowlist = []string{"success", "rejected", "failure"}

// Recorder records auth-library business metrics through an injected
// Prometheus registerer so a long-running consumer (API Server, Console
// forwarder) can expose the families from its own /metrics endpoint
// without creating a second listener or competing for the global
// default registry.
//
// Recorder owns *Vec references for the families it knows how to
// update. Methods that derive label values from caller input route
// through io.astrasync/control-plane/observability/normalize so every
// Recorder-emitting path enforces identical label-allowlist rules
// (ADR-058 §3).
type Recorder struct {
	SignInTotal        *prometheus.CounterVec
	SessionRevokeTotal *prometheus.CounterVec
}

// NewRecorder registers an isolated auth-library metrics recorder with
// the supplied registerer. The supplied registerer must not have
// registered the same metric name already (prometheus returns
// "already registered" on collision).
//
// The Recorder's CounterVec are independent from the package-level
// AuthSignInTotal / AuthSessionRevokeTotal so a long-running consumer
// can host the Recorder without competing with the process-global
// scrape surface that Handler() continues to expose.
func NewRecorder(registerer prometheus.Registerer) (*Recorder, error) {
	if registerer == nil {
		return nil, ErrNilRegisterer
	}
	recorder := &Recorder{
		SignInTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "auth_sign_in_total",
			Help: "Total sign-in flows served by the auth library.",
		}, []string{"tenant_id", "outcome"}),
		SessionRevokeTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "auth_session_revoke_total",
			Help: "Total session revocations served by the auth library.",
		}, []string{"tenant_id"}),
	}
	for _, collector := range []prometheus.Collector{recorder.SignInTotal, recorder.SessionRevokeTotal} {
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
// registerer already registered a metric with the same name (prometheus
// reports "duplicate metrics collector registration attempted"). The
// error exposes a sentinel value so callers can detect the collision
// without string-matching on the wrapped prometheus message.
var ErrDuplicateMetric = &RecorderError{reason: "register auth metric"}

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
// can build a single sentinel for "duplicate metric" without importing
// prometheus.
func (e *RecorderError) Wrap(cause error) *RecorderError {
	return &RecorderError{reason: e.reason, cause: cause}
}

// ObserveSignIn records one auth-library sign-in flow. tenantID and
// outcome route through normalize so the slice 43.2 label contract is
// identical to every other tenant-deriving Recorder in the control
// plane (ADR-058 §3). requestID is recorded verbatim — the auth flow
// surface does not yet have a documented allowlist for request IDs, so
// the recorder does not normalize. Calls with a nil receiver are
// silently dropped so callers can pass a Recorder only at the
// boundary sites that own it.
func (r *Recorder) ObserveSignIn(tenantID, outcome, requestID string) {
	if r == nil || r.SignInTotal == nil {
		return
	}
	r.SignInTotal.WithLabelValues(
		normalize.NormalizeTenant(tenantID),
		normalize.NormalizeOutcome(outcome, authOutcomeAllowlist, "failure"),
	).Inc()
}

// ObserveSessionRevoke records one auth-library session revocation.
// The admin CLI `revoke-session` operation is the documented
// slice-43.2 call site (ADR-058 §2). tenantID routes through
// normalize; the auth revoke metric has no outcome label. Calls with
// a nil receiver are silently dropped.
func (r *Recorder) ObserveSessionRevoke(tenantID, requestID string) {
	if r == nil || r.SessionRevokeTotal == nil {
		return
	}
	r.SessionRevokeTotal.WithLabelValues(
		normalize.NormalizeTenant(tenantID),
	).Inc()
}

// Handler returns the Prometheus HTTP handler that scrapes the global
// default registerer (the same surface that AuthSignInTotal and
// AuthSessionRevokeTotal are auto-registered against). The slice 43.2
// design keeps this entry point stable so any existing import
// (including the legacy metrics_test.go and the authmetrics package
// README) continues to scrape the same series.
func Handler() http.Handler {
	return promhttp.Handler()
}

// HandlerFor returns a Prometheus HTTP handler that scrapes the
// supplied gatherer. Long-running consumers (API Server, Console
// forwarder) host a Recorder through their own registerer and expose
// the Recorder-owned metrics through this handler instead of through
// Handler().
func HandlerFor(gatherer prometheus.Gatherer) http.Handler {
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{EnableOpenMetrics: true})
}
