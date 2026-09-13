// Package auth is the public Go API for the AstraSync auth library.
// This file re-exports the Prometheus Recorder from
// io.astrasync/control-plane/auth/internal/authmetrics so callers
// outside the auth module (Console BFF, API Server) can wire the
// sign-in / session-revoke emission paths documented in
// ADR-058 §2 and the Phase 17 / Phase 22 / Phase 23 emission slices.
//
// The internal package owns the registration logic (Recorder,
// NewRecorder, ObserveSignIn, ObserveSessionRevoke). The public
// re-export is intentionally thin — callers should not duplicate
// the package-level CounterVec surface; they should construct one
// Recorder per prometheus.Registerer and inject it via
// functional options (e.g. authflow.WithRecorder).
package auth

import (
	"github.com/prometheus/client_golang/prometheus"

	"io.astrasync/control-plane/auth/internal/authmetrics"
)

// Recorder is the public re-export of the authmetrics Recorder.
// Callers (Console BFF, API Server, admin CLI) own one Recorder per
// prometheus.Registerer so the Recorder-owned CounterVec surface is
// exposed from that registerer's /metrics endpoint. Methods are
// nil-safe; passing a nil Recorder at the call boundary is
// intentionally a no-op.
type Recorder = authmetrics.Recorder

// NewRecorder registers the auth-library sign-in and session-revoke
// CounterVec families against the supplied prometheus.Registerer.
// The registerer must not have registered the same metric name
// already; the duplicate-registration error is reported through
// authmetrics.ErrDuplicateMetric.
//
// The Recorder is independent from the package-level CounterVec
// surface (AuthSignInTotal / AuthSessionRevokeTotal) so a
// long-running consumer can host the Recorder without competing
// with the process-global scrape surface.
func NewRecorder(registerer prometheus.Registerer) (*Recorder, error) {
	return authmetrics.NewRecorder(registerer)
}

// ErrNilRegisterer is the sentinel error returned by NewRecorder
// when the caller passes a nil prometheus.Registerer.
var ErrNilRegisterer = authmetrics.ErrNilRegisterer

// ErrDuplicateMetric is the sentinel error returned by NewRecorder
// when the supplied registerer already registered a metric with
// the same name.
var ErrDuplicateMetric = authmetrics.ErrDuplicateMetric
