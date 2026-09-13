# ADR-074: Phase 29 — API Server Consumes `x-astra-tenant-id` (Server-side Interceptor + `job.Job.tenant_id` Column)

## Status

Accepted

## Context

Phase 28 closed two halves of the tenant-id envelope contract on the **egress**
boundary (ADR-072) and the **Kubernetes admission** boundary (ADR-071), and
Slice 28-B (ADR-073) closed the **SyncJob CR creation** half. But the
**server-side consumption** half — what happens after the API Server receives a
mutating RPC carrying `x-astra-tenant-id` — has not been wired. The metadata
arrives at the API Server today, the authn interceptor does not look at it, and
the `job.Job` PostgreSQL row carries no `tenant_id` column. The downstream
metric labels that motivated ADR-071 / 072 (`controller_job_state_total`,
`controller_epoch_fence_total`) still see `_unknown` for every Console-issued
job because the API Server cannot reconstruct the tenant context that the BFF
already had in its hands.

The current chain:

1. Console BFF — ADR-072 §1 — appends `x-astra-tenant-id` to outgoing gRPC
   metadata on every mutation path. Tested in `bff_slice28_test.go`.
2. API Server authn interceptor — today — extracts only `authorization` and
   `x-request-id`. `x-astra-tenant-id` arrives but is silently dropped.
3. JobService.newJobMutation — `job_mutation_service.go` line 247 — calls
   `tenantIDForConnectionUse(ctx, key.Namespace)` which derives TenantID from
   `principal.MembershipForScope(scope)`. This works for the API Server's own
   caller but is **silent**: the authn interceptor has no guarantee the
   declared tenant context matches the BFF-supplied one.
4. PostgreSQL `astrasync_control_jobs` — `001_jobs.sql` — has **no
   `tenant_id` column**. The mutation repository writes `mutation.TenantID`
   into `astrasync_job_idempotency` (002_job_mutations.sql) and
   `astrasync_job_tombstones` (002), but the public row is keyed only by
   `(namespace, name)`. There is no durable per-row tenant binding that the
   controller can join against.

There are two structural reasons to consume `x-astra-tenant-id` server-side
now rather than later:

1. **Trust boundary:** today the API Server derives `TenantID` from the
   authenticated principal's membership for the request's `Namespace` (the
   `Scope` function on `Policy`). This is correct for the API Server's own
   caller. But it does not survive a future where a tenant-id-bound surface
   (the Console BFF today; potentially a future region-promotion controller or
   CLI bootstrap) calls the API Server. The metadata is the durable signal
   that ties the request to the calling tenant.
2. **Cross-region audit:** ADR-050 §3 establishes that the `tenant_id` column
   on `astrasync_security_audit_events` is the source of truth for tenant-
   scoped audit. Today the JobService writes `mutation.TenantID` into the audit
   row. The derived-from-membership value is correct **iff** the principal's
   membership is fresh. A server-side metadata check makes the contract
   explicit: if the metadata disagrees with the membership, the request is
   denied — no silent fallthrough.

The migration is small enough to land as one slice. The slice reuses the
existing `tenantIDForConnectionUse` helper as a fallback for non-BFF callers
(such as the lifecycle controller, which calls `repository.Update` directly
without metadata) and adds a new `authn.JobTenantIDFromIncomingMetadata` /
`authn.JobTenantIDFromContext` extraction point that the interceptor consults
when `x-astra-tenant-id` is present.

## Decision

### 1. New `tenant_id` column on `astrasync_control_jobs`

Migration `control-plane/job/postgres/migrations/003_jobs_tenant_id.sql`
adds a nullable `tenant_id UUID` column to `astrasync_control_jobs`. The
column is **nullable** because:

1. Existing rows pre-Phase-29 have no tenant binding. Backfilling requires a
   `JOIN` against `astrasync_auth_tenants` on `namespace`, and that JOIN
   silently maps to the wrong row when a single namespace is shared by two
   tenants (this happens in test fixtures but should not happen in
   production; the safe posture is to leave the column nullable until a
   later migration backfills). The backfill is out of scope for this ADR
   and is documented in `docs/phase29/README.md` as a follow-up.
2. The `Repository.Create` and `Repository.Update` paths remain usable by
   the lifecycle controller (which already writes the right value into the
   `tenant_id` column on the audit row) without forcing every existing call
   site to learn the new field.

A `CHECK` constraint enforces `tenant_id IS NOT NULL OR namespace NOT IN
(<reserved namespaces>)` once the backfill lands; the constraint is **not**
added in this migration so existing rows continue to satisfy the schema.

### 2. `authn.JobTenantIDFromIncomingMetadata` — canonical UUID extractor

A new file `control-plane/api-server/internal/authn/tenant_metadata.go`
exposes:

```go
const TenantMetadataKey = "x-astra-tenant-id"

func JobTenantIDFromIncomingMetadata(ctx context.Context) (string, bool, error) {
    values := metadata.ValueFromIncomingContext(ctx, TenantMetadataKey)
    if len(values) == 0 {
        return "", false, nil
    }
    if len(values) != 1 {
        return "", false, fmt.Errorf("x-astra-tenant-id must appear at most once")
    }
    raw := strings.TrimSpace(values[0])
    if _, err := uuid.Parse(raw); err != nil {
        return "", false, fmt.Errorf("x-astra-tenant-id must be a canonical UUID")
    }
    return raw, true, nil
}

type jobTenantIDContextKey struct{}

func withJobTenantID(ctx context.Context, tenantID string) context.Context {
    return context.WithValue(ctx, jobTenantIDContextKey{}, tenantID)
}

func JobTenantIDFromContext(ctx context.Context) string {
    if ctx == nil {
        return ""
    }
    value, _ := ctx.Value(jobTenantIDContextKey{}).(string)
    return value
}
```

The extractor is a **pure function** that does not validate against any
principal. Reconciliation happens in the interceptor (§3).

### 3. Interceptor reconciliation — single trust boundary

After the existing membership resolution
(`principal.MembershipForScope(scope)` returns an active membership), the
interceptor performs:

```
if metadataPresent {
    if !uuid.Match(metadataTenantID) { reject PermissionDenied("tenant envelope malformed") }
    if membership.TenantID != metadataTenantID { reject PermissionDenied("tenant envelope mismatch") }
    withJobTenantID(ctx, metadataTenantID)
} else {
    withJobTenantID(ctx, membership.TenantID)  // fallback for non-BFF callers
}
```

The `withJobTenantID` call attaches the canonical UUID into the request
context. The handler chain (`JobService.newJobMutation` →
`tenantIDForConnectionUse`) reads the attached value **first**; only if the
context is empty does it fall back to the membership-derived value (the
existing helper behaviour, which survives for the lifecycle controller's
direct repository calls).

The audit row written by `writeJobMutationAudit` (mutation_repository.go
line 552) continues to use `mutation.TenantID`; that value now flows from
the verified metadata-or-membership resolution, so the audit row's
`tenant_id` is the same value the API Server authorised the request for.

### 4. Tenant-id is the new mutation `Mutation.TenantID`

`newJobMutation` is updated to call `authn.JobTenantIDFromContext` first and
fall back to `tenantIDForConnectionUse(ctx, key.Namespace)` if empty. The
fallback preserves the existing behaviour for direct (non-BFF) callers. The
mutation repository already validates `m.TenantID` is a canonical UUID
(`mutation.go` line 134), so no validation changes are needed downstream.

### 5. Repository writes the column

The PostgreSQL `Repository.Create`, `Repository.Update`, and
`lockJob` / `updateLockedJob` helpers are extended to project the
`mutation.TenantID` value through the existing scanner. The repository
contract does **not** add `TenantID` to the public `job.Job` struct (the
`job.Job` is identified by `(namespace, name)` and the tenant binding is a
write-side concern; exposing it would surface an internal invariant into the
domain model that no handler currently needs).

For `Memory.Repository`, the tenant binding is captured implicitly via the
`MutationRepository` interface — the in-memory implementation does not need
to track it because there is no multi-tenant test that depends on it. The
in-memory repository is documented as **not** enforcing tenant-id binding
in this slice (see §6 for the testing implications).

### 6. Hard reject on metadata-vs-membership mismatch

A new test `TestInterceptorRejectsTenantMetadataMismatch` exercises:

- BFF sends `x-astra-tenant-id: <tenantA>` against `namespace=tenantA`.
- Principal has `Membership{ TenantID: tenantB, TenantNamespace: tenantA }`.
- The interceptor must reject with `codes.PermissionDenied` and audit
  `TENANT_DENIED`.

This is the **defence-in-depth** posture from ADR-072 §2 mirrored on the
server side. It catches a misconfigured BFF that proxies a stale
session-bound tenant, and it catches a principal whose membership was
rotated to a different tenant mid-session.

## Consequences

### Positive

- The API Server now has an explicit, durable tenant envelope on every
  mutation. The `job.Job` PostgreSQL row carries `tenant_id`, which the
  controller can join against `astrasync_auth_tenants` to recover tenant
  context without re-deriving it from the principal session.
- The hard-reject on metadata-vs-membership mismatch closes the silent
  fallthrough class of bug. If a future caller rotates memberships
  mid-session, the API Server fails closed rather than writing rows under
  the wrong tenant.
- The migration is small and forward-compatible. Existing rows are not
  blocked by the new nullable column; new rows get the tenant binding.
- No new dependency. `metadata.ValueFromIncomingContext` and `uuid.Parse`
  are already imported across the authn module.
- The interceptor test surface stays the same: 5 lines of new code in
  `interceptor.go`, 1 new test in `interceptor_test.go`, 1 new file in
  `authn/tenant_metadata.go`. The change is local and reviewable.

### Negative

- The `tenant_id` column on `astrasync_control_jobs` is **nullable** for
  the duration of the backfill window. The lifecycle controller's direct
  `Repository.Update` calls (e.g. for status transitions) do not write
  `tenant_id` today; they will start writing it after this slice ships,
  but rows written before the slice are nullable. A follow-up ADR will
  backfill and tighten the NOT NULL constraint.
- The `Memory.Repository` does not enforce tenant binding in this slice.
  The unit tests for `JobService` continue to use the in-memory repository
  without setting `Mutation.TenantID` (the existing `newJobMutation`
  fallback still computes it from the membership, which is sufficient for
  unit tests). The integration tests in `mutation_integration_test.go`
  already pass `TenantID` explicitly; they will continue to pass.
- The fallback path in `newJobMutation` means there are still two ways to
  compute `Mutation.TenantID`. A future slice should consolidate to the
  metadata-only path once every caller (lifecycle controller, audit
  replayer, region-promotion controller) is known to send the metadata.

## Alternatives Considered

### Do not add `tenant_id` to `astrasync_control_jobs`; rely on the audit row

The audit row already has `tenant_id` (since the mutation repository writes
`mutation.TenantID` into `astrasync_security_audit_events`). A future query
that joins audit + job by `(uid, audit_event_id)` could reconstruct the
binding. But this is a **read-side** reconstruction: it is expensive, it
relies on every query going through audit, and it leaks the audit boundary
into job read paths. A `tenant_id` column on the public row is the simpler
contract.

### Reject the BFF metadata entirely; let the principal session be the only source

Today `tenantIDForConnectionUse` derives TenantID from the principal
membership, which is correct for direct callers but does not survive a
BFF. Rejecting the metadata would force the BFF to *also* call the API
Server as a user (which it already does, but the tenant envelope would
have to round-trip through token claims, which is a bigger change). The
metadata is the smallest durable signal.

### Defer Phase 29 to after a dedicated tenant-binding ADR

A "tenant binding" ADR could explore namespace-vs-tenant-id partitioning
in depth. But ADR-071 and ADR-072 already established that the tenant-id
is the canonical binding. Phase 29 is the consumer of those two contracts;
it does not redefine them.

### Make `tenant_id` NOT NULL immediately

A NOT NULL constraint would force every existing call site to set
`tenant_id` on `INSERT`, which is a larger blast radius. The nullable
column is the incremental compromise; the backfill ADR will tighten it.