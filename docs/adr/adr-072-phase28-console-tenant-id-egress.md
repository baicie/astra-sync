# ADR-072: Phase 28 — Console BFF Forwards `x-astra-tenant-id` on Job Mutations

## Status

Accepted

## Context

ADR-071 (§4) lands the `astrasync.io/tenant-id` kubebuilder validation on the
SyncJob CR. The validation prevents a SyncJob CR from being admitted without
a canonical-lowercase UUID label. The contract hinges on the Console being the
single user-facing surface that creates SyncJob resources, because the Console
has the authenticated principal's tenant context already.

The Console currently does **not** create Kubernetes `SyncJob` CRs. It calls
the API Server `JobService.CreateJob` gRPC, which writes a `job.Job` row to
PostgreSQL; no Kubernetes write is performed. So while ADR-071's CRD
validation is in place, there is nothing forcing Console-side code to capture
the tenant context on the mutation wire. The label injection is implied by
the ADR but not yet enforced at the BFF layer.

Until the Console gains a controller-runtime client (a separate Phase 28-B
slice that needs K8s RBAC, in-cluster config, and a dual-write reconciliation
strategy with the `job.Job` PostgreSQL row), every job mutation goes through
the BFF without an explicit tenant-id envelope. Downstream:

- `controller_job_state_total` and `controller_epoch_fence_total` continue to
  emit `_unknown` for every SyncJob whose CR is later created without the
  label, even though the Console session **knows** the tenant.
- There is no contract that guarantees a Console mutation carries the
  authoritative tenant context to the control plane. Any future SyncJob CR
  write that lands without a label has no upstream audit trail to identify
  the responsible principal.

The Phase 23/24/25 emission metrics rely on the controller reading the label
from the SyncJob CR (ADR-066 §Key Design Decisions). The Console is the only
party that simultaneously holds:

1. The authenticated session principal with a bound `tenantID`.
2. The PostgreSQL `job.Job` write path through `JobService.CreateJob`.

It is the correct boundary for the tenant-id to cross.

## Decision

### 1. Slice 28-A: BFF tenant-id egress (this ADR)

The Console BFF forwards `x-astra-tenant-id` as outgoing gRPC metadata on
every **mutation** path that creates or transitions a job:

- `POST /api/jobs` (`createJob`)
- `PUT /api/jobs/{name}` (`updateJob`)
- `DELETE /api/jobs/{name}` (`deleteJob`)
- `POST /api/jobs/{name}/start` (`startJob`)
- `POST /api/jobs/{name}/stop` (`stopJob`)
- `POST /api/jobs/{name}/validate` (`validateJob`)

The metadata key matches ADR-071's label-style convention (`x-astra-tenant-id`,
kebab-case, lowercase). Read-only endpoints (`GET /api/jobs{,/...}`) do **not**
carry the metadata — the gRPC server derives tenant context from the request
proto's `Namespace` field, and additional metadata would create ambiguity.

### 2. Hard reject when scope lacks tenant-id

The BFF must **fail closed** when the resolved scope's `tenantID` is empty or
otherwise non-canonical-lowercase UUID. The mutation handlers
(`createJob`, `updateJob`, `deleteJob`, `startJob`, `stopJob`, `validateJob`)
emit `codes.PermissionDenied("tenant scope denied")` and **must not forward
to the backend** if `scope.tenantID` is empty. The backend never sees a
mutation request that lacks tenant context; this is the architectural
guarantee that `_unknown` cannot leak into the metrics labels.

The check is performed **after** `s.scope(request)` resolves the membership
session and **before** any `s.mutations.*` call. It is a defense-in-depth
measure on top of `s.scope`'s own empty-tenant rejection path (server.go
line 442-490).

### 3. Idempotency-Key continuity

The existing `Idempotency-Key` header → `IdempotencyKey` proto field forward
remains the source of truth (job_handlers.go `idempotencyKey(request)`). The
new `x-astra-tenant-id` metadata does **not** displace or duplicate the
idempotency key; both travel on the same `backendContext`. The
`Idempotency-Key` continues to enforce 16-128 char bounds and rejects control
characters, while the tenant-id metadata carries no length-bounded payload
beyond the canonical UUID format enforced at session resolution.

### 4. Slice 28-B (future, not this ADR)

A subsequent ADR will land the Console's controller-runtime client and the
`SyncJob` CR write path that injects `astrasync.io/tenant-id` from the same
session-bound `tenantID`. Slice 28-B depends on:

- New `console/go.mod` dependency: `sigs.k8s.io/controller-runtime` +
  Kubernetes API machinery.
- A `console/internal/syncjobcr/` package owning the `client.Client` lifecycle
  and K8s CRUD with tenant-id label injection.
- A dual-write reconciliation: PostgreSQL `job.Job` (authoritative) vs K8s
  `SyncJob` (cached). Failure modes (which write comes first, what happens on
  K8s outage) require a separate ADR-039-style walkthrough and are explicitly
  out of scope for Slice 28-A.

Until 28-B ships, mutations complete the `job.Job` PostgreSQL write without
creating a corresponding `SyncJob` CR; the upstream tenant-id label contract
is enforced at the **BFF egress** boundary rather than the **CR admission**
boundary. This is a deliberate, incremental compromise: any future local-dev
or `kubectl`-built CR still fails CRD validation, but every Console-issued
mutation now carries the tenant-id envelope, ready for the API Server to
consume in a Phase 29 server-side interceptor.

## Consequences

### Positive

- Every Console mutation now exposes the authoritative `tenantID` to the
  control plane via `x-astra-tenant-id` outgoing metadata. The hard reject
  prevents `_unknown` from leaking into future server-side metrics that key
  on this metadata.
- The contract is testable at the BFF layer with a memory-only fake backend
  (`metadata.FromIncomingContext`), so `go test ./console/internal/server/...`
  can verify without spinning up a real gRPC server.
- No new dependency in `console/go.mod`. Slice 28-A is independent of
  controller-runtime and can ship before the larger K8s wiring work.
- The failure mode is bounded: if `x-astra-tenant-id` is dropped between the
  BFF and the API Server, the **mutation is rejected**, not silently
  forwarded without tenant context. This matches the project's
  defence-in-depth posture (ADR-037, ADR-043).

### Negative

- The `x-astra-tenant-id` metadata is currently a **Console-only** signal.
  The API Server does not yet read it (Phase 29 work). Until a server-side
  interceptor consumes the metadata, the metric labels that motivated this
  ADR (`controller_job_state_total` etc.) are unchanged; tenants still
  appear as `_unknown` for SyncJobs created without `kubectl`-side
  cooperation.
- The hard reject adds one extra branch to each mutation handler. The
  five added checks (`createJob`, `updateJob`, `deleteJob`, `startJob`,
  `stopJob`, `validateJob`) are simple and isolated, but a future handler
  added without the check would re-introduce the gap. A regression test
  in `bff_slice28_test.go` covers each path; future handlers must add a
  corresponding test.
- `metadata.FromIncomingContext` requires the gRPC server side to extract
  the value, but the test fixture uses in-process invocation so the
  metadata flow is exercised via `metadata.AppendToOutgoingContext`. Any
  transport change (HTTP/2 → in-process) that strips outgoing metadata
  would invalidate this slice's tests; the slice itself is intentionally
  transport-agnostic to keep the contract testable.

## Alternatives Considered

### Reject the BFF layer change; do everything in the K8s client (Slice 28-B)

Land a controller-runtime client in the Console first, then have the
Console write SyncJob CRs with the label. Skip the BFF-level metadata
forward.

**Reject.** This bundles the tenant-id envelope contract with the dual-write
risk surface (K8s write failures, RBAC misconfiguration). The BFF egress
contract is a small, low-risk slice that can land independently. It also
provides an immediate test target for any future server-side tenant
interceptor (Phase 29) without coupling to K8s wiring.

### Pass tenant-id via existing `Namespace` field on the proto

Stuff the tenant UUID into the existing `request.Namespace` string field,
since the API Server already derives tenant context from namespace.

**Reject.** Namespace and tenant-id are distinct concepts (one tenant can
own multiple namespaces; the field is a K8s namespace string, not an
identity). Conflating them would corrupt namespace semantics and require a
post-Phase-29 reconciliation to undo. A dedicated metadata key is
explicit, additive, and reversible.

### Console-side session token carries the tenant-id without re-emission

The OIDC access token already encodes the tenant via claims. Rely on the
API Server to parse the bearer token to extract `tenant_id`.

**Reject.** The API Server uses OIDC opaque tokens; the access-token
introspection happens via the `auth` module, not the request context. A
metadata-based forward is orthogonal to token claims and survives token
rotation without explicit coordination.

### Add a new proto field `tenant_id` to `CreateJobRequest`

Modify `api/protobuf/v1/job.proto` to add `string tenant_id = N;`. Wire it
through the API Server's mutation path.

**Reject.** Tighter coupling than necessary for an additive metadata signal.
The proto field approach requires regenerating Java + Go (`make proto-generate`),
rewriting the API Server's `CreateJob` server-side handler, and would
need a parallel ADR on schema evolution. The metadata approach reuses the
existing gRPC metadata facility and is migration-free.
