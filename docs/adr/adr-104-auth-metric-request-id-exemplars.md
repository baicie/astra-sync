# ADR-104: Auth Metric Request ID Exemplars

## Status

Accepted

## Context

ADR-047 defines a bounded `request_id` exemplar label and F7 applies it to the
API Server authentication and audit-query families. The sign-in and
session-revoke metric owners already received request IDs, but their Recorder
methods ignored those values. The API Server session-revoke metric also did
not receive the request ID used by its audit row.

The missing values prevent operators from moving directly from those counters
to the matching authentication, session-revocation, or audit records.

## Decision

1. Apply the existing canonical-lowercase-UUID exemplar contract to
   `apiserver_sign_in_total`, `apiserver_session_revoke_total`,
   `auth_sign_in_total`, and `auth_session_revoke_total`.
2. Keep `request_id` out of normal metric labels. Invalid, missing, uppercase,
   compact, or otherwise non-canonical values emit the ordinary counter
   sample without an exemplar.
3. Compute the API Server session-revocation request ID once before writing
   the audit event, then pass that same value to both the audit row and every
   per-tenant metric observation.
4. Preserve the Console BFF and auth admin CLI request IDs already supplied to
   the auth-library Recorder. The CLI remains sample-only when its supplied
   request ID is not canonical.
5. Keep existing metric names, normal labels, outcomes, and registration
   behavior unchanged.

The change completes call-site coverage under ADR-047; it does not introduce
a new identity source or protocol field.

## Consequences

- Authentication and session-revocation spikes can link directly to the
  matching log and audit records.
- API Server session-revocation metrics and audit rows use one request ID for
  the same operation.
- Metric time-series cardinality remains bounded because request IDs are
  exemplar-only.
- Existing callers that provide no request ID remain compatible and produce
  ordinary samples.
- No dependency, deployment, protocol, storage, or dashboard-query change is
  required.

## Rollback

Remove the exemplar calls from the four auth-related Recorder methods and
restore the API Server session-revoke metric call to its two-argument form.
Counters and normal labels remain otherwise unchanged.
