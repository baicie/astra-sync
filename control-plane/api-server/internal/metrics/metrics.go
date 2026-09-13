// Package metrics registers Prometheus descriptors for API Server business
// metrics and provides the recorder and /metrics handler used by call sites.
// The names and labels follow docs/observability/metrics-catalog.md.
//
// Label normalization rules live in io.astrasync/control-plane/observability/normalize
// (see ADR-058 §3). Every Recorder method that derives a label value from
// caller input MUST route through that package; package-level CounterVec
// access is restricted to tests and the registration boundary.
package metrics

import (
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"io.astrasync/control-plane/observability/normalize"
)

// AuthRequestTotal counts authentication decisions per tenant.
var AuthRequestTotal = promauto.NewCounterVec(authRequestTotalOpts(), []string{"tenant_id", "outcome"})

// AuthRequestDuration records the latency of authentication decisions.
var AuthRequestDuration = promauto.NewHistogramVec(authRequestDurationOpts(), []string{"tenant_id", "outcome"})

// SignInTotal counts OIDC sign-in flows.
var SignInTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "apiserver_sign_in_total",
	Help: "Total OIDC sign-in flows initiated.",
}, []string{"tenant_id", "outcome"})

// SessionRevokeTotal counts session revocation calls.
var SessionRevokeTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "apiserver_session_revoke_total",
	Help: "Total session revocation calls served by the API Server.",
}, []string{"tenant_id", "actor_id"})

// AuditQueryDuration records the latency of audit event queries.
var AuditQueryDuration = promauto.NewHistogramVec(auditQueryDurationOpts(), []string{"tenant_id"})

// TrustedProxyHSTS counts HSTS responses emitted by the trusted-proxy layer.
var TrustedProxyHSTS = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "apiserver_trusted_proxy_hsts_total",
	Help: "Total HSTS responses served by the trusted-proxy layer.",
}, []string{"tenant_id"})

// signInOutcomeAllowlist enumerates the outcome label values accepted for
// apiserver_sign_in_total. The list mirrors the catalog authentication
// outcome allowlist (docs/observability/metrics-catalog.md, "The
// authentication outcome allowlist" paragraph). Values outside the list
// collapse to "failure" through normalize.NormalizeOutcome so a runaway
// caller cannot inflate cardinality.
var signInOutcomeAllowlist = []string{"success", "rejected", "failure"}

// Recorder updates the API Server metric families owned by authenticated
// business call sites.
//
// The Recorder owns *Vec references for the families it knows how to
// update. Methods that derive label values from caller input route
// through io.astrasync/control-plane/observability/normalize so every
// Recorder-emitting path enforces identical label-allowlist rules
// (ADR-058 §3).
type Recorder struct {
	authRequestTotal    *prometheus.CounterVec
	authRequestDuration *prometheus.HistogramVec
	auditQueryDuration  *prometheus.HistogramVec
	signInTotal         *prometheus.CounterVec
	sessionRevokeTotal  *prometheus.CounterVec
	trustedProxyHSTS    *prometheus.CounterVec
}

// NewRecorder registers an isolated API Server SLO recorder. Production uses
// DefaultRecorder; this constructor supports explicit registries in tests and
// embedded runtimes.
func NewRecorder(registerer prometheus.Registerer) (*Recorder, error) {
	if registerer == nil {
		return nil, fmt.Errorf("metrics registerer must not be nil")
	}
	recorder := &Recorder{
		authRequestTotal: prometheus.NewCounterVec(
			authRequestTotalOpts(), []string{"tenant_id", "outcome"},
		),
		authRequestDuration: prometheus.NewHistogramVec(
			authRequestDurationOpts(), []string{"tenant_id", "outcome"},
		),
		auditQueryDuration: prometheus.NewHistogramVec(
			auditQueryDurationOpts(), []string{"tenant_id"},
		),
		signInTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "apiserver_sign_in_total",
				Help: "Total OIDC sign-in flows initiated.",
			}, []string{"tenant_id", "outcome"},
		),
		sessionRevokeTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "apiserver_session_revoke_total",
				Help: "Total session revocation calls served by the API Server.",
			}, []string{"tenant_id", "actor_id"},
		),
		trustedProxyHSTS: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "apiserver_trusted_proxy_hsts_total",
				Help: "Total HSTS responses served by the trusted-proxy layer.",
			}, []string{"tenant_id"},
		),
	}
	for _, collector := range []prometheus.Collector{
		recorder.authRequestTotal, recorder.authRequestDuration, recorder.auditQueryDuration,
		recorder.signInTotal, recorder.sessionRevokeTotal, recorder.trustedProxyHSTS,
	} {
		if err := registerer.Register(collector); err != nil {
			return nil, fmt.Errorf("register API Server SLO metric: %w", err)
		}
	}
	return recorder, nil
}

// DefaultRecorder returns a recorder backed by the process-global metric
// families exposed by Handler.
func DefaultRecorder() *Recorder {
	return &Recorder{
		authRequestTotal:    AuthRequestTotal,
		authRequestDuration: AuthRequestDuration,
		auditQueryDuration:  AuditQueryDuration,
		signInTotal:         SignInTotal,
		sessionRevokeTotal:  SessionRevokeTotal,
		trustedProxyHSTS:    TrustedProxyHSTS,
	}
}

// ObserveAuthRequest records one completed authentication and authorization
// decision. requestID is attached only when it is a canonical UUID.
func (r *Recorder) ObserveAuthRequest(tenantID, outcome, requestID string, duration time.Duration) {
	if r == nil {
		return
	}
	exemplar := requestExemplar(requestID)
	counter := r.authRequestTotal.WithLabelValues(tenantID, outcome)
	if adder, ok := counter.(prometheus.ExemplarAdder); ok && exemplar != nil {
		adder.AddWithExemplar(1, exemplar)
	} else {
		counter.Inc()
	}
	observe(r.authRequestDuration.WithLabelValues(tenantID, outcome), duration, exemplar)
}

// ObserveAuditQuery records one authorized audit query. requestID is attached
// only when it is a canonical UUID.
func (r *Recorder) ObserveAuditQuery(tenantID, requestID string, duration time.Duration) {
	if r == nil {
		return
	}
	observe(r.auditQueryDuration.WithLabelValues(tenantID), duration, requestExemplar(requestID))
}

// ObserveSignIn records one OIDC sign-in flow outcome. tenantID and outcome
// are normalised before they become label values:
//
//   - tenantID: only canonical lowercase UUIDs and the fixed "_platform"
//     sentinel are accepted; everything else becomes "_unknown".
//   - outcome: only the catalog allowlist (success | rejected | failure)
//     is accepted; everything else collapses to "failure".
//
// requestID is attached only when it is a canonical UUID.
func (r *Recorder) ObserveSignIn(tenantID, outcome, requestID string) {
	if r == nil {
		return
	}
	normalizedTenant := normalize.NormalizeTenant(tenantID)
	normalizedOutcome := normalize.NormalizeOutcome(outcome, signInOutcomeAllowlist, "failure")
	r.signInTotal.WithLabelValues(normalizedTenant, normalizedOutcome).Inc()
}

// ObserveSessionRevoke records one session revocation. actorID is treated
// as a worker-id-shaped label: trimmed, length-bounded at 128 bytes, and
// collapsed to "_unknown" on overflow so two runaway callers cannot
// collide on a truncated label.
func (r *Recorder) ObserveSessionRevoke(tenantID, actorID string) {
	if r == nil {
		return
	}
	normalizedTenant := normalize.NormalizeTenant(tenantID)
	normalizedActor := normalize.NormalizeWorkerID(actorID)
	r.sessionRevokeTotal.WithLabelValues(normalizedTenant, normalizedActor).Inc()
}

// ObserveTrustedProxyHSTS records one HSTS response served by the
// trusted-proxy layer. The pre-auth tenant scope resolves to "_unknown"
// because the trusted-proxy layer runs before any tenant claim is
// parsed; the helper still routes through normalize.NormalizeTenant so
// the slice is the single source of truth for label-value handling.
func (r *Recorder) ObserveTrustedProxyHSTS(tenantID string) {
	if r == nil {
		return
	}
	normalizedTenant := normalize.NormalizeTenant(tenantID)
	r.trustedProxyHSTS.WithLabelValues(normalizedTenant).Inc()
}

func observe(observer prometheus.Observer, duration time.Duration, exemplar prometheus.Labels) {
	if duration < 0 {
		duration = 0
	}
	seconds := duration.Seconds()
	if exemplarObserver, ok := observer.(prometheus.ExemplarObserver); ok && exemplar != nil {
		exemplarObserver.ObserveWithExemplar(seconds, exemplar)
		return
	}
	observer.Observe(seconds)
}

func requestExemplar(requestID string) prometheus.Labels {
	parsed, err := uuid.Parse(requestID)
	if err != nil || parsed.String() != requestID {
		return nil
	}
	return prometheus.Labels{"request_id": requestID}
}

func authRequestTotalOpts() prometheus.CounterOpts {
	return prometheus.CounterOpts{
		Name: "apiserver_auth_request_total",
		Help: "Total authentication requests served by the API Server.",
	}
}

func authRequestDurationOpts() prometheus.HistogramOpts {
	return prometheus.HistogramOpts{
		Name:    "apiserver_auth_request_duration_seconds",
		Help:    "Latency of authentication requests served by the API Server.",
		Buckets: prometheus.DefBuckets,
	}
}

func auditQueryDurationOpts() prometheus.HistogramOpts {
	return prometheus.HistogramOpts{
		Name:    "apiserver_audit_query_duration_seconds",
		Help:    "Latency of audit event queries served by the API Server.",
		Buckets: prometheus.DefBuckets,
	}
}

// Handler returns the OpenMetrics-capable HTTP handler that scrapes the
// process-global gatherer.
func Handler() http.Handler {
	return HandlerFor(prometheus.DefaultGatherer)
}

// HandlerFor returns an OpenMetrics-capable handler for the supplied gatherer.
func HandlerFor(gatherer prometheus.Gatherer) http.Handler {
	return handler(gatherer)
}

func handler(gatherer prometheus.Gatherer) http.Handler {
	return promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{EnableOpenMetrics: true})
}
