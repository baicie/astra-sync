// Package authmetrics registers the auth library Prometheus metrics. The
// metrics are exported from the package so that future API-Server-side
// admin RPCs and the API Server authentication layer can register the
// same way. The admin CLI does not bind a /metrics port (it is a
// one-shot command); the metrics package remains importable.
package authmetrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// AuthSignInTotal counts sign-in outcomes served by the auth library.
var AuthSignInTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "auth_sign_in_total",
	Help: "Total sign-in flows served by the auth library.",
}, []string{"tenant_id", "outcome"})

// AuthSessionRevokeTotal counts session revocations served by the auth library.
var AuthSessionRevokeTotal = promauto.NewCounterVec(prometheus.CounterOpts{
	Name: "auth_session_revoke_total",
	Help: "Total session revocations served by the auth library.",
}, []string{"tenant_id"})

// Handler returns the Prometheus HTTP handler that scrapes the global
// default registerer.
func Handler() http.Handler {
	return promhttp.Handler()
}
