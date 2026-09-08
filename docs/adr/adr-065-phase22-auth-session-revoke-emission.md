# ADR-065: Phase 22 Emission Sub-Slice 43.2.5 — auth_session_revoke_total via admin CLI

## Status

Proposed

## Context

ADR-058 §2 documents the `auth_session_revoke_total` counter with
the following call site:

> the admin CLI `revoke-session` success boundary is the documented
> slice-43.2.5 call site (Recorder wired; integration pending
> because the admin CLI is one-shot).

ADR-061 §133 and ADR-063 §3 confirm this as a Phase 22+ candidate
with the open question: "admin CLI Recorder needs the long-running
consumer question resolved."

The long-running-consumer question has two parts:

1. **Tenant ID derivation**: the `auth_session_revoke_total` metric
   has a `tenant_id` label (metrics-catalog.md), but the sessions
   table (`astrasync_auth_sessions`) has no `tenant_id` column.
   A principal can hold memberships in multiple tenants
   (`astrasync_auth_memberships` PK = `(tenant_id, principal_id)`).
   The correct emission semantics are: observe **once per unique
   tenant** the principal belongs to, because a `revoke-session`
   call revokes all sessions for that principal across all tenants.

2. **Scrape path for a one-shot CLI**: the admin CLI is a
   classic offline utility that performs one idempotent operation
   and exits. It cannot serve a `/metrics` endpoint in the
   traditional Prometheus pull model. The slice-43.2 Recorder
   design (§"long-running consumer" paragraph) already anticipates
   this: a long-running consumer (API Server, Console forwarder)
   hosts the Recorder through its own registerer. For the
   one-shot admin CLI, the alternative is a **log-dump emission
   pattern**: after the revocation succeeds, the CLI logs the
   metric observation in structured JSON (one line per tenant)
   before exiting, allowing Grafana Agent / Prometheus log-based
   service discovery to collect the sample. This satisfies the
   "emitted" milestone in metrics-catalog.md without requiring the
   admin CLI to become a long-running service.

## Decision

### Slice 48.1: Repository — tenant ID lookup for revoke

Add `LoadTenantIDsForPrincipal(ctx, principalID string) ([]string, error)`
to `authpostgres.Repository`. The method queries
`astrasync_auth_memberships` for all ACTIVE tenant memberships of the
principal and returns the unique tenant IDs. Returns an empty slice if
the principal has no active memberships (the revoke still proceeds;
the metric observation is skipped for that tenant slot).

This method is internal to the repository package and is not exposed
as a gRPC or CLI flag. The name follows the existing
`LoadTenantMembers` pattern.

### Slice 48.2: Repository — extend RevokeSessionsForPrincipal return type

`RevokeSessionsForPrincipal` currently returns `(int64, error)` —
the number of rows deleted. Extend the return to
`(int64, []string, error)` where `[]string` is the list of active
tenant IDs for the principal (derived by calling
`LoadTenantIDsForPrincipal` within the same database transaction).

The transaction scope: `BEGIN → SELECT tenant_ids → DELETE sessions →
COMMIT`. Both reads and the delete are in the same transaction so
the count and tenant list are consistent.

### Slice 48.3: Admin CLI — wire Recorder + log-dump emission

In `control-plane/auth/cmd/admin/main.go`, add the following
changes for the `revoke-session` operation:

**Dependency**: import `io.astrasync/control-plane/auth/internal/authmetrics`
and `github.com/prometheus/client_golang/prometheus`.

**Registry**: create `prometheus.NewRegistry()` at the start of
`revokeSessions`. Register `authmetrics.NewRecorder(registry)` and
store the `Recorder` on `adminCommand` (add a `recorder *authmetrics.Recorder`
field; inject via constructor option or on the `adminCommand` struct
directly for the one-shot pattern).

**Call site**: after `repository.RevokeSessionsForPrincipal` returns
successfully (count ≥ 0, err == nil), call
`r.recorder.ObserveSessionRevoke(tenantID, requestID)` for each
tenant ID in the returned tenant list. If the tenant list is empty,
no observation is made (the operation succeeded but no active
memberships existed; this is not an error).

**Log-dump**: before `adminCommand` exits (in `main()` after
`command.run(...)` returns), if the operation was `revoke-session`
and succeeded, dump the registry's current Gatherer's content to
structured log output (`slog` at info level, one JSON line per
metric family). The format is:

```
{"level":"info","msg":"authmetrics dump","operation":"revoke-session",
 "tenant_count":2,"registry":"<tenant_id_1>,<tenant_id_2>"}
```

This is not Prometheus scrape output; it is a structured confirmation
that the Recorder was called for each tenant. The `registry` field
lists the tenant IDs that received an observation. Grafana Agent can
parse these lines and translate them into Prometheus samples.

**Test**: add a `revokeSession_wiresRecorder` test in
`authmetrics/metrics_test.go` (black-box) that:
- calls `RevokeSessionsForPrincipal` against a fake repository
  (pre-seeded with two active memberships for one principal)
- asserts the Recorder was called twice (once per tenant)
- asserts each observation carried the correct normalized tenant ID

### Slice 48.4: Metrics catalog update

`docs/observability/metrics-catalog.md`
`auth_session_revoke_total` row status: change from

> "Recorder wired in Phase 17 slice 43.2 (ADR-058); integration
> pending because the admin CLI is one-shot"

to:

> "Recorder wired in Phase 17 slice 43.2 (ADR-058); Phase 22 slice
> 48 (ADR-065) observes at the admin CLI `revoke-session` success
> boundary. A `prometheus.Registry` is created per invocation; a
> structured log line confirms the observation per tenant. The
> one-shot CLI uses log-dump emission; a long-running consumer
> (API Server, Console) can host the same Recorder for traditional
> scrape emission. tenant_id is derived by joining sessions to
> memberships (one observation per unique tenant the principal
> holds an active membership in)."

## Non-Goals

- The admin CLI does not gain a `/metrics` HTTP endpoint. The
  one-shot design (offline utility) is preserved.
- The `auth_sign_in_total` emission (slice 43.1.5) is not in scope.
  It requires a new API Server sign-in handler + RBAC role
  (AGENTS.md §8 decision gate) and is a separate Phase 22+
  candidate.
- The `controller_job_state_total` / `controller_epoch_fence_total`
  emission (slice 43.3.5) is not in scope. It requires a durable
  commit decision (ADR-053 / ADR-026) and is a separate Phase 22+
  candidate.
- The Java data-plane emission (ADR-051 §7 `26.F9`) is not in scope.
  It is a Phase 23+ candidate per ADR-063 §3.

## Consequences

### Positive

- `auth_session_revoke_total` moves from "Recorder wired" to
  "emitted" in the metrics catalog, completing the Phase 17
  activation matrix for the auth-library family.
- The admin CLI `revoke-session` operation now has a measurable
  outcome signal: operators can alert on revocation rate per tenant
  even from the one-shot CLI.
- The log-dump pattern is documented as an acceptable emission
  alternative for one-shot batch/CLI jobs, extending the
  slice-43.2 design without requiring a daemon.
- The repository change (`[]string` return) is general-purpose:
  other operations that need tenant context for a principal can
  reuse `LoadTenantIDsForPrincipal`.
- The `RevokeSessionsForPrincipal` transaction is consistent:
  count and tenant list are always from the same snapshot.

### Negative

- The log-dump pattern is a non-standard Prometheus emission path.
  It requires Grafana Agent / log-based service discovery to be
  configured. If that collector is not deployed, the metric
  observation is lost. The catalog row documents this explicitly.
- One principal in N tenants → N metric observations. This matches
  the revoke semantics (all sessions revoked regardless of tenant)
  but the total count is `N × count_per_tenant`, not
  `count_total`. Dashboard queries must account for this
  (divide-by-N or filter by tenant_id).
- The one-shot `prometheus.Registry` per invocation means
  `auth_session_revoke_total` resets to 0 on each admin CLI run.
  Historical rate queries (e.g. `rate(auth_session_revoke_total[5m])`)
  only work when a long-running consumer (API Server, Console) hosts
  the Recorder. This limitation is documented in the catalog row.
- The Phase 22 scope is narrow (one metric, one call site). The
  Phase 17 activation matrix is not fully complete until slices
  43.1.5 and 43.3.5 also land.

## Alternatives Considered

### Admin CLI becomes a long-running daemon with `/metrics`

Reject. The admin CLI is an offline utility. Converting it to a
daemon changes its operational model, requires a service supervisor
(systemd/K8s), and conflicts with the "offline operator utility"
design documented in its own package comment. The log-dump pattern
achieves the same emission goal without this change.

### Pushgateway for one-shot metric emission

Reject. Pushgateway is designed for batch jobs that push their
metrics at job completion. It requires admin CLI to depend on
`github.com/prometheus/client_golang/prometheus/push` and to know
the Pushgateway address (new flag / env var). The log-dump pattern
requires no additional dependency and no new configuration surface;
structured logging is already present in the CLI.

### API Server owns auth_session_revoke_total instead of admin CLI

Reject. The API Server does not own the session-revoke operation
for admin CLI-initiated revocations (ADR-058 §2 call site is
"admin CLI `revoke-session`"). API Server can additionally observe
session revocations that originate from Console (if a Console
forwarder is added in a future slice), but that does not replace
the admin CLI emission.

### Observe once globally (no tenant_id label for admin CLI)

Reject. The metrics-catalog.md documents `tenant_id` as a label for
`auth_session_revoke_total`. Dropping it would break the catalog
contract and make per-tenant alerting impossible. The per-tenant
observation is the correct design.
