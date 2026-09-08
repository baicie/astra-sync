# ADR-068: Phase 24 Emission Sub-Slice 43.1.5 — apiserver_session_revoke_total via API Server revoke RPC

## Status

Accepted

## Context

ADR-058 §2 documents the `apiserver_session_revoke_total` counter with
the following call site:

> "new API Server session-revoke RPC introduced by slice 1, or Console
> forwarder".

ADR-065 §1 and ADR-067 §"Consequences" record this as a Phase 24+
candidate with the open question: "API Server sign-in handler needs new
RPC + RBAC role — AGENTS.md §8 decision gate".

The `apiserver_session_revoke_total` metric has been **Recorder-wired**
since Phase 17 slice 43.1 (ADR-058). The `ObserveSessionRevoke`
method exists in `api-server/internal/metrics.Recorder` and routes all
label values through `io.astrasync/control-plane/observability/normalize`.
The production **call site** — the API Server gRPC handler — is the
missing piece.

The auth library already exposes
`RevokeSessionsForPrincipal(ctx, principalID)` in
`authpostgres.Repository` (Phase 22 slice 48.1, ADR-065). The admin CLI
uses this method to revoke all Console sessions for one principal
through the `auth_session_revoke_total` family. The API Server can
expose a similar RPC that invokes the same repository method through
the `AccessService`, emitting `apiserver_session_revoke_total`.

Two label semantics:

1. **`tenant_id`**: the API Server caller (an admin) does not know the
   target principal's tenant membership. The repository method returns
   the unique active tenant IDs of the target principal; the API Server
   emits `apiserver_session_revoke_total` once per unique tenant
   (mirrors Phase 22 / ADR-065 admin-CLI behaviour).
2. **`actor_id`**: the API Server caller's principal UUID, normalized
   through `NormalizeWorkerID` (worker-id-shaped label, ADR-058 §3).

The `outcome` label is not part of `apiserver_session_revoke_total`
(the metric family documents only `tenant_id` and `actor_id`). The
Recorder method signature is
`ObserveSessionRevoke(tenantID, actorID string)`.

The API Server gRPC surface for tenant-scoped admin operations already
includes `RevokeTenantRole` (revokes an RBAC membership) and
`RevokePlatformRole` (revokes a platform role). The new
`RevokeConsoleSession` RPC belongs to the same family — it is an
admin-level "revoke all sessions for one principal" operation.

## Decision

### Slice 50.0: ADR + index entry

This ADR + an entry in `docs/adr/README.md`.

### Slice 50.1: Protobuf surface — `RevokeConsoleSession` RPC

Add a new RPC to the `AccessService` proto package
(`api/protobuf/v1/identity.proto` or the existing
`api/protobuf/v1/access.proto` if present):

```protobuf
service AccessService {
  ...
  // RevokeConsoleSession deletes every active Console session for the
  // given principal. The call is idempotent: a second call with the
  // same principal_id and idempotency_key is a no-op. The API Server
  // emits apiserver_session_revoke_total once per active tenant the
  // target principal holds a membership in. The actor_id label is the
  // authenticated caller's principal UUID.
  //
  // Required permission: PermissionMembersManage on the caller's
  // tenant scope (admin-level operation, mirrors RevokeTenantRole).
  rpc RevokeConsoleSession(RevokeConsoleSessionRequest) returns (RevokeConsoleSessionResponse);
}

message RevokeConsoleSessionRequest {
  string principal_id = 1;
  // Idempotency key, 16-128 chars (mirrors the access-service
  // idempotency contract, ADR-038).
  string idempotency_key = 2;
}

message RevokeConsoleSessionResponse {
  // Number of Console sessions deleted (zero if the principal has
  // no active sessions or the call is a replay).
  int64 sessions_revoked = 1;
  // Number of distinct active tenants the target principal held
  // memberships in (matches the apiserver_session_revoke_total
  // series count).
  int32 tenant_count = 2;
}
```

The new RPC is **append-only**: it does not modify any existing
field number or change existing RPC semantics. The proto file change
follows the ADR-012 strict-versioned-job-spec boundary.

Run `make proto-generate` to regenerate the Go / Java bindings. Commit
the generated code alongside the proto change.

### Slice 50.2: Access service — wire Repository.RevokeSessionsForPrincipal + Recorder.ObserveSessionRevoke

In `control-plane/api-server/internal/service/access_service.go`:

1. Add a new method `RevokeConsoleSession` to the `AccessRepository`
   interface:
   ```go
   RevokeConsoleSessionsForPrincipal(
       ctx context.Context, principalID, actorID string,
       audit auth.SecurityAuditEvent,
   ) (int64, []string, error)
   ```
   This method wraps the existing
   `authpostgres.Repository.RevokeSessionsForPrincipal` and threads the
   `SecurityAuditEvent` (audit row written in the same transaction,
   per ADR-037 / ADR-041).

2. Implement the wrapping in
   `control-plane/auth/postgres/repository.go::SessionStoreAdapter`
   (or a new file `auth/postgres/access_adapter.go` — implementation
   choice deferred to the slice PR).

3. Add a `revokeRecorder *apiMetrics.Recorder` field to `AccessService`
   and a `WithAccessRevokeRecorder(recorder *apiMetrics.Recorder)`
   functional option. The recorder is nil-safe (slice 43.1 contract).

4. Implement the gRPC handler:
   ```go
   func (s *AccessService) RevokeConsoleSession(
       ctx context.Context, request *controlv1.RevokeConsoleSessionRequest,
   ) (*controlv1.RevokeConsoleSessionResponse, error) {
       if request == nil {
           return nil, status.Error(codes.InvalidArgument, "request must not be nil")
       }
       if _, err := tenantIDPatternFromString(request.GetPrincipalId()); err != nil {
           return nil, status.Errorf(codes.InvalidArgument, "principal_id is invalid: %v", err)
       }
       if err := validateIdempotencyKey(request.GetIdempotencyKey()); err != nil {
           return nil, err
       }
       decision, err := s.authorize(ctx, "", auth.PermissionMembersManage)
       if err != nil {
           return nil, err
       }
       actorID := principalActorID(decision)
       auditEvent := auth.SecurityAuditEvent{
           EventID: s.uid(), EventType: "access.console_session.revoked",
           ActorID: actorID, TenantID: "",
           RequestID: accessAuditRequestID(ctx, s.uid),
           Outcome:   "CHANGED",
           Attributes: map[string]any{
               "principalId":    request.GetPrincipalId(),
               "idempotencyKey": request.GetIdempotencyKey(),
           },
           OccurredAt: s.now().UTC(),
       }
       count, tenantIDs, err := s.repository.RevokeConsoleSessionsForPrincipal(
           ctx, request.GetPrincipalId(), actorID, auditEvent,
       )
       if err != nil {
           return nil, accessRepositoryError(err)
       }
       for _, tenantID := range tenantIDs {
           s.revokeRecorder.ObserveSessionRevoke(tenantID, actorID)
       }
       return &controlv1.RevokeConsoleSessionResponse{
           SessionsRevoked: count,
           TenantCount:     int32(len(tenantIDs)),
       }, nil
   }
   ```

5. The `authorize(ctx, "", ...)` call resolves to a **platform-level**
   permission check (no tenant scope). The platform_admin role is
   required; tenant-level admins cannot invoke this RPC. The
   `PrincipalFromContext` lookup confirms the caller is a platform_admin.

### Slice 50.3: Wire Recorder in `cmd/server/main.go`

In `control-plane/api-server/cmd/server/main.go`, pass the existing
`metricRecorder` (already a `*apiMetrics.Recorder`) to
`NewAccessService`:

```go
accessService, err := service.NewAccessService(authRepository, authorizer,
    service.WithAccessClock(time.Now),
    service.WithAccessUIDSource(uuid.NewString),
    service.WithAccessRevokeRecorder(metricRecorder),
)
```

### Slice 50.4: Metrics catalog update

`docs/observability/metrics-catalog.md`
`apiserver_session_revoke_total` row status: change from

> "Recorder wired in Phase 17 slice 43.1 (ADR-058); production call
> site pending. The Recorder routes every label value through
> `normalize` so the slice-43.1 contract is enforced even before the
> production call site lands."

to:

> "Recorder wired in Phase 17 slice 43.1 (ADR-058); Phase 24 slice 50
> (ADR-068) observes at the API Server `RevokeConsoleSession` RPC
> success boundary. The RPC requires the platform_admin role; the
> handler invokes `authpostgres.Repository.RevokeSessionsForPrincipal`
> (Phase 22 / ADR-065) and emits `apiserver_session_revoke_total` once
> per unique active tenant the target principal holds a membership
> in. The `tenant_id` label is the active tenant; the `actor_id`
> label is the authenticated caller's principal UUID (normalized via
> `normalize.NormalizeWorkerID`)."

## Non-Goals

- A new audit event type **is** introduced (`access.console_session.revoked`),
  but it follows the existing ADR-037 transactional audit pattern.
  The audit event is a new string in the `EventType` column; it does
  not require a new column or a schema change.
- A new RBAC role **is** introduced: the platform_admin role
  (`auth.RolePlatformAdmin` if not already defined, else the existing
  identifier). This is a documentation / discovery change, not a
  schema change.
- The Console BFF forwarder path (alternative to the new RPC) is not
  in scope. The new RPC is the documented call site per ADR-058 §2.
- The `controller_epoch_fence_total` emission (slice 49.3.5) is not
  in scope. It remains Phase 24+ pending ADR-053 §3.
- The Java data-plane emission (ADR-051 §7 `26.F9`) is not in scope.

## Consequences

### Positive

- `apiserver_session_revoke_total` moves from "Recorder wired" to
  "emitted" in the metrics catalog, completing the Phase 17 activation
  matrix for the API Server family.
- The API Server gains a single canonical admin operation for
  revoking Console sessions: `RevokeConsoleSession`. Operators no
  longer need to log in to a control-plane host to run the admin
  CLI `revoke-session` command.
- The handler reuses the existing
  `authpostgres.Repository.RevokeSessionsForPrincipal` (Phase 22 / ADR-065),
  so the per-tenant emission semantics (one observation per unique
  active tenant) and the serializable transaction design (consistent
  snapshot of count + tenant IDs) are shared with the admin CLI path.
- The audit row `access.console_session.revoked` is written in the
  same transaction as the session deletes (per ADR-037), satisfying
  the transactional audit trail invariant.
- The proto change is **append-only**: no existing field numbers or
  RPCs are modified, satisfying ADR-012 compatibility rules.

### Negative

- The new RPC introduces a new audit event type
  (`access.console_session.revoked`). Existing audit-query consumers
  must be aware of the new event type. The Phase 22 audit-query
  filter list (`docs/observability/metrics-catalog.md` §"Authentication
  and authorization metrics" or `docs/architecture.md` §audit) should
  be updated to include the new type.
- The `principal_id` field is required; an empty or malformed
  principal ID is rejected. The validation reuses the existing
  `tenantIDPatternFromString` parser (UUID-only).
- The platform_admin requirement limits the RPC to platform-level
  admins. Tenant-level admins still use the admin CLI. A future
  enhancement (Phase 24+) can relax this with explicit
  cross-tenant authorization, but that requires a multi-tenant admin
  RBAC role that does not exist today.
- The Console BFF currently handles Console session management
  (login, refresh, logout) but not bulk session revocation. The new
  API Server RPC is the canonical platform-level path; the Console
  BFF does not gain a forwarder (per ADR-058 §2 decision: API Server
  RPC, not forwarder).

## Alternatives Considered

### Console BFF forwarder (admin CLI path through Console)

Reject. The forwarder pattern requires the Console BFF to invoke the
admin CLI's `revoke-session` logic via an HTTP/grpc boundary, adding
a translation layer and a deployment coupling (the Console BFF would
need to bundle the auth library's revoke logic). The new API Server
RPC is a direct gRPC call into the auth repository, simpler and more
testable.

### API Server `access.console_session.revoked` as a non-transactional log line

Reject. ADR-037 requires "数据行 + 审计行" same-transaction. A
non-transactional log line would break the audit invariant.

### Add a `tenant_id` parameter to scope the revoke to one tenant

Reject. The repository's `RevokeSessionsForPrincipal` deletes all
sessions for the principal across all tenants. Adding a tenant filter
at the API Server layer is a new feature (tenant-scoped revoke),
not in scope for the emission sub-slice. The metrics are emitted
per tenant automatically because the repository returns the tenant
list.

### Reuse the existing `RevokeTenantRole` RPC semantics with `principal_id=`

Reject. `RevokeTenantRole` revokes a tenant membership, not Console
sessions. The two operations write to different tables
(`astrasync_auth_memberships` vs `astrasync_auth_sessions`) and have
different audit semantics. A separate RPC is correct.

### Wait for the Controller `controller_epoch_fence_total` emission sub-slice

Reject. The Phase 17 activation matrix is incomplete; closing one
emission sub-slice at a time keeps each slice small and reviewable.
The two pending emission sub-slices (apiserver_session_revoke_total
and controller_epoch_fence_total) have different dependencies
(API Server RPC vs ADR-053 durable commit decision) and can land
independently.

## References

- ADR-012 — Strict Versioned JobSpec Boundary
- ADR-037 — Transactional Control-plane Audit Trail
- ADR-038 — Desired-state Job Mutation Workflows
- ADR-058 — Observability Catalog Backlog (Phase 17 umbrella)
- ADR-065 — Phase 22 Emission Sub-Slice 43.2.5 (admin CLI revoke)
- ADR-067 — v0.7.0 Release Cut
