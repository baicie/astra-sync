// Package authmetrics defines Prometheus descriptors for the auth library.
// Future server-side auth call sites can import the package and create samples.
// The one-shot admin CLI neither imports this package nor binds a /metrics port.
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
