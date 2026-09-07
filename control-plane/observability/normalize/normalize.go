// Package normalize centralises the label-allowlist rules that govern
// AstraSync control-plane Prometheus business metrics. The package is
// intentionally small: it owns the canonical normalisation helper for
// tenant identifiers, bounded outcome allowlists, and worker identifiers
// so that every metric package (auth, api-server, controller, scheduler,
// observability) emits identical label values and cannot drift.
//
// The package deliberately has no Prometheus dependency. It exposes pure
// functions so that any metric owner can call them without pulling the
// client_golang registry into unrelated code paths.
//
// Reference: ADR-058 (Observability Catalog Backlog, Phase 17), §3.
package normalize

import (
	"strings"

	"github.com/google/uuid"
)

// UnknownTenant is the fixed label value emitted when caller input cannot
// be parsed as a canonical tenant UUID. The value MUST stay exactly
// "_unknown" because the Operator dashboard recipes bind to it (see
// docs/observability/dashboard-recipes.md).
const UnknownTenant = "_unknown"

// PlatformTenant is the fixed label value emitted for self-scope methods
// that do not have a tenant UUID in their scope (e.g. platform_admin RBAC
// grants). The value MUST stay exactly "_platform".
const PlatformTenant = "_platform"

// UnknownWorker is the fixed worker label value emitted when the caller
// did not supply a worker identity (or supplied a value outside the
// length allowlist).
const UnknownWorker = "_unknown"

// workerIDMaxLen bounds the worker-id label cardinality. The Prometheus
// client cap is not enforced; we bound here so a runaway caller cannot
// emit arbitrary-length series.
const workerIDMaxLen = 128

// NormalizeTenant returns the canonical tenant label value:
//   - when value parses as a canonical lowercase UUID and round-trips
//     through uuid.Parse, value is returned unchanged;
//   - when value equals "_platform" (the platform self-scope), value is
//     returned unchanged;
//   - otherwise _unknown is returned.
//
// The round-trip check guards against uppercase UUIDs, braces, dashes in
// unusual positions, and other strings that uuid.Parse tolerates but that
// the catalog disallows (ADR-047 §126, "canonical lowercase UUID").
func NormalizeTenant(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return UnknownTenant
	}
	if trimmed == PlatformTenant {
		return PlatformTenant
	}
	parsed, err := uuid.Parse(trimmed)
	if err != nil {
		return UnknownTenant
	}
	if parsed.String() != trimmed {
		// uuid.Parse is permissive: it accepts uppercase hex and a few
		// brace forms. The catalog only allows the canonical lowercase
		// form, so anything that does not round-trip exactly is dropped.
		return UnknownTenant
	}
	return trimmed
}

// NormalizeOutcome returns the canonical outcome label value:
//   - when value is in allowed, value is returned unchanged;
//   - otherwise fallback is returned.
//
// allowed MUST be a small, finite allowlist (three to five values). The
// function does NOT lowercase or trim the input: callers should pre-trim
// if they expect that. Empty input is rejected through fallback.
func NormalizeOutcome(value string, allowed []string, fallback string) string {
	trimmed := strings.TrimSpace(value)
	for _, candidate := range allowed {
		if candidate == trimmed {
			return trimmed
		}
	}
	return fallback
}

// NormalizeWorkerID returns the canonical worker label value:
//   - empty values and values that exceed workerIDMaxLen bytes (after
//     trimming) become _unknown;
//   - otherwise the trimmed value is returned.
//
// Long values are dropped rather than truncated so that two distinct
// callers cannot collide on the same truncated label.
func NormalizeWorkerID(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || len(trimmed) > workerIDMaxLen {
		return UnknownWorker
	}
	return trimmed
}
