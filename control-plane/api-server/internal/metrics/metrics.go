// Package metrics registers Prometheus descriptors for API Server business
// metrics and provides its /metrics handler. Business call sites create the
// samples. The names and labels follow the convention documented at
// docs/observability/metrics-catalog.md. Each metric is registered
// exactly once through promauto.NewCounterVec against the global
// prometheus.DefaultRegisterer.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// AuthRequestTotal counts authentication decisions per tenant.
var AuthRequestTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "apiserver_auth_request_total",
	Help: "Total authentication requests served by the API Server.",
}, []string{"tenant_id", "outcome"})

// AuthRequestDuration records the latency of authentication decisions.
var AuthRequestDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "apiserver_auth_request_duration_seconds",
	Help:    "Latency of authentication requests served by the API Server.",
	Buckets: prometheus.DefBuckets,
}, []string{"tenant_id", "outcome"})

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
var AuditQueryDuration = promauto.NewHistogramVec(prometheus.HistogramOpts{
	Name:    "apiserver_audit_query_duration_seconds",
	Help:    "Latency of audit event queries served by the API Server.",
	Buckets: prometheus.DefBuckets,
}, []string{"tenant_id"})

// TrustedProxyHSTS counts HSTS responses emitted by the trusted-proxy layer.
var TrustedProxyHSTS = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "apiserver_trusted_proxy_hsts_total",
	Help: "Total HSTS responses served by the trusted-proxy layer.",
}, []string{"tenant_id"})

// Handler returns the Prometheus HTTP handler that scrapes the global
// default registerer.
func Handler() http.Handler {
	return promhttp.Handler()
}
