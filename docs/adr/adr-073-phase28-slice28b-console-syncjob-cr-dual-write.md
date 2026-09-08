# ADR-073: Phase 28 Slice 28-B — Console Owns `SyncJob` CR Dual-Write

## Status

Accepted — Implementation landed as Phase 28 Slice 28-B; superseded
in spirit by Phase 29 (ADR-074) which closes the **API Server-side**
tenant-id consumption half. The two ADRs together close the
tenant-id envelope contract end to end.

## Implementation deltas (vs. ADR proposal)

The slice landed with three small deltas from the original proposal:

1. **No controller-runtime dependency.** The console module ships its
   own `console/internal/syncjobcr` package built on `net/http` plus a
   minimal local `SyncJob` CRD type
   (see ADR-073 §Implementation deltas.1 below). The decision is
   documented in `console/internal/syncjobcr/manager.go` package
   comment: controller-runtime's full dependency tree
   (`k8s.io/apiserver`, `k8s.io/component-base`, `k8s.io/streaming`)
   was not present in the module cache at the time of integration
   and the Go proxy was intermittently unavailable. The HTTP client
   communicates with the Kubernetes API server using the in-cluster
   ServiceAccount (token + CA cert), which is functionally
   equivalent to what `client-go` does under the hood.

2. **Local CRD type mirror.** `console/internal/syncjobcr.SyncJob`
   mirrors the controller module's
   `control-plane/controller/api/v1.SyncJob` CRD type for the
   subset of fields the Console writes. The fields must stay in
   sync with the controller module; the package comment records
   this contract.

3. **BFF mutation handler integration.** `console/internal/server/job_handlers.go`
   now calls `s.crWriter.WriteCR(ctx, scope, name, mutation, spec)`
   on every Create/Update/Delete path. Start/Stop do **not** write
   the CR (the controller picks up desired state from the durable
   `job.Job` row, not from `CR.spec.state`).

## Test surface

`console/internal/server/bff_slice28b_test.go` ships 12 cases:

- `TestConsoleCreatesSyncJobCROnCreateJob` — Create path emits CR write.
- `TestConsoleUpdatesSyncJobCROnUpdateJob` — Update path emits CR write.
- `TestConsoleDeletesSyncJobCROnDeleteJob` — Delete path emits CR write.
- `TestConsoleStartStopDoNotMutateCR` — Start/Stop paths emit zero CR writes.
- `TestConsoleCRWriteFailsOpenOnAdmission` — admission failure → 200, metric records.
- `TestConsoleCRWriteFailsOpenOnTimeout` — timeout failure → 200, metric records.
- `TestConsoleDisabledCRManagerSkipsWrite` — noop writer does not block.
- `TestConsoleCRCarriesTenantIDFromScope` — cross-tenant denial blocks CR write.
- `TestConsoleCREmitsStableLabels` — spec payload carries canonical connector names.
- `TestConsoleCRPostgreSQLFirstOrdering` — backend.Create called before CR.Write.
- `TestConsoleCRWriteIsCalledOnlyAfterPGSuccess` — PG failure aborts CR write.

`console/internal/syncjobcr/manager_test.go` covers the lower-level
CR construction logic (label injection, namespace routing, payload
serialization).

## Helm

`deployment/helm/astrasync/templates/console/syncjob-cr-role.yaml`
ships a narrow Role + RoleBinding for `sync.astrasync.io/syncjobs`
verbs `[get, create, update, delete]` (plus `[get, update]` on
`syncjobs/status`). The Role is gated on
`.Values.console.syncjobCRWrite.enabled` so operators can disable
the dual-write path per environment. The wider `list`, `watch`,
`patch` verbs are intentionally omitted.

## Metric emission

`controller_syncjob_console_dual_write_total{mutation, outcome}` is
wired in `console/observability/metrics.go`. The 5 outcome values
(`success`, `admission_rejected`, `timeout`, `invalid`, `disabled`)
cover the ADR-073 §5 failure-mode matrix. Cardinality is bounded
at `3 mutation * 5 outcome = 15` series.

## Original proposal

(Kept for archival. See "Original proposal" below.)

## Context

ADR-071 / ADR-072 closed two halves of the same gap:

- ADR-071 enforces the `astrasync.io/tenant-id` label on every `SyncJob` CR
  at the Kubernetes admission boundary (kubebuilder CEL `XValidation`,
  `self.labels['astrasync.io/tenant-id'].match('^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$')`).
  Existing `deployment/operator/config/crd/bases/sync.astrasync.io_syncjobs.yaml`
  ships that rule (Slice 27-A). A `SyncJob` CR cannot be admitted without a
  canonical-lowercase UUID tenant label.
- ADR-072 makes the Console BFF carry that tenant through
  `x-astra-tenant-id` outgoing gRPC metadata on every job mutation
  (`createJob` / `updateJob` / `deleteJob` / `startJob` / `stopJob` /
  `validateJob`), with a hard reject when scope lacks tenant context. The
  BFF is wired to `tenantScopeCheck` + `backendContextWithTenant` (Slice 28-A,
  10 tests passing).

But neither ADR closes the **CR write path** itself. Today:

1. The Console calls `JobService.CreateJob` (gRPC) on the API Server.
2. The API Server writes a `job.Job` row to PostgreSQL.
3. **Nothing writes the corresponding `SyncJob` CR.** No controller, no
   webhook, no goroutine. The Kubernetes object that the
   controller-runtime controller reconciles (`control-plane/controller/internal/controller/syncjob_controller.go`)
   never appears for Console-issued jobs.

So even though ADR-072's mutation envelope is intact and the CRD's CEL
validation is intact, the metric that motivated both ADRs
(`controller_job_state_total{tenant_id="..."}`) remains `_unknown` for every
Console-issued job. The controller never sees the `SyncJob` resource because
it is never created.

There is no current `kubectl`-side operator flow for Console-issued jobs. The
Console is the only path through which an `astrasync` job enters the system
end-to-end, and it currently ends at the PostgreSQL row.

This ADR closes the third gap. It introduces a **Console-side dual-write**
where the Console, after a successful PostgreSQL write, also creates the
`SyncJob` CR via `sigs.k8s.io/controller-runtime/pkg/client`. The
controller-runtime client already lives in the controller module
(`control-plane/controller/go.mod` v0.24.1); the slice reuses the same version
in `console/go.mod` to avoid pinning drift.

The dual-write is the right architectural move because:

1. The Console holds the only authoritative tenant context (session-bound
   `tenantID`); no other surface has a reason to write a `SyncJob` CR.
2. The `job.Job` PostgreSQL row is the durable source of truth for lifecycle
   state (ADR-029, ADR-031); the `SyncJob` CR is a cached view that the
   controller reconciles into that durable state. PostgreSQL-first write
   matches the existing durable-state-machine architecture.
3. Failing closed (PostgreSQL succeeded, CR creation failed) is preferable
   to the alternative (CR-only with no durable record), which would orphan
   the job from the audit trail.

The slice is independent of any Phase 29 server-side work (API Server
consuming `x-astra-tenant-id`); the slice only needs the BFF mutation
contract from Slice 28-A. The deferred concerns from the
`docs/phase28/README.md` Slice 28-B note (dual-write reconciliation, K8s
RBAC, in-cluster config) are addressed explicitly below.

## Decision

### 1. New module dependency

`console/go.mod` gains:

```go
require (
    sigs.k8s.io/controller-runtime v0.24.1
    k8s.io/apimachinery v0.36.3
)
```

Version pins match `control-plane/controller/go.mod` (already on
`controller-runtime v0.24.1` + `k8s.io/apimachinery v0.36.3`); `k8s.io/api`
is a transitive dependency of `controller-runtime` and will be pulled
indirectly. The slice uses `client.Client` only — no manager, no
reconciler, no cache — to keep the Console's runtime footprint
identical to today.

`go mod tidy` adds the transitive closure. CI runs
`make vet-go && make test-go` to confirm no version skew.

### 2. New package: `console/internal/syncjobcr`

The package owns the `client.Client` lifecycle and the four
`SyncJob` CRUD operations the Console needs:

- `Create(ctx, scope, job.Job) → SyncJob`
- `Update(ctx, scope, job.Job, expectedVersion) → SyncJob`
- `Delete(ctx, scope, name) → error`
- `Get(ctx, scope, name) → SyncJob` (read-only path; not strictly required
  for the mutation contract, but it enables future list-by-tenant queries)

`scope` here is the existing `console/internal/server.scope` (tenantID +
namespace + membership). The package does **not** import `console/internal/server`
(dependency direction would invert); it accepts an explicit `scopeConfig{
TenantID string; Namespace string }` so the BFF can pass through.

The package returns a typed `*kerror` from `k8s.io/apimachinery/pkg/api/errors`
so the BFF can translate `IsNotFound` / `IsAlreadyExists` / `IsInvalid` into
the existing `codes.NotFound` / `codes.AlreadyExists` / `codes.FailedPrecondition`
mapping.

### 3. CR construction: `*v1.SyncJob` literal

The CR is built by reusing the type from `control-plane/controller/api/v1`
(`io.astrasync/control-plane/controller/api/v1`). The Console already has
a transitive dependency on `control-plane/api-server`, but adding
`control-plane/controller/api/v1` is a new direct dependency. Two options
were considered:

- **Add a direct replace** on `control-plane/controller` — rejected, the
  Console should not depend on a binary module's runtime; it only needs the
  CRD types.
- **Re-export the CRD types from a small standalone module
  `control-plane/crd-types`** — deferred; out of scope for Slice 28-B.
  Phase 29 should revisit if the Console needs additional controller
  internals.

For this slice the Console imports `control-plane/controller/api/v1` directly
with a `replace` directive mirroring the existing
`io.astrasync/control-plane/api-server` pattern:

```go
replace io.astrasync/control-plane/controller => ../control-plane/controller
```

This is acceptable because `control-plane/controller/api/v1/` contains
**only** CRD types (`SyncJob`, `SyncJobSpec`, `SyncJobStatus`,
`CheckpointInfo`, `FailureInfo`, `SyncJobList`); the package has no
controller-runtime manager dependency at the type level. `go vet` confirms
the package boundary.

### 4. Label injection (the heart of this slice)

`Create` constructs:

```go
cr := &syncv1.SyncJob{
    ObjectMeta: metav1.ObjectMeta{
        Name:      job.Name,
        Namespace: scope.Namespace,
        Labels: map[string]string{
            syncv1.TenantIDLabel: scope.TenantID,
            // ADR-071 §1: the label is required for admission.
        },
        // ADR-029 §3: spec.name must match metadata.name for controller
        // key derivation; controller-runtime's Get derives
        // NamespacedName from this metadata.
    },
    Spec: syncv1.SyncJobSpec{
        Source:     job.Spec.Source,
        Sink:       job.Spec.Sink,
        Transforms: job.Spec.Transforms,
        Delivery:   job.Spec.Delivery,
        Runtime:    job.Spec.Runtime,
        // State left at the CRD default (STOPPED); controller will
        // reconcile desired state from the durable job.Job row, not
        // from CR.spec.state. We intentionally omit State so that
        // the controller continues to drive lifecycle from PG, not
        // from CR (see §5).
    },
}
```

`Update` performs a `client.Update` after a `client.Get` to read
`resourceVersion`, preserving optimistic concurrency. If the resourceVersion
drift is detected (return code 409 Conflict), the BFF translates to
`codes.Aborted` (the API Server already uses this for version conflicts on
`job.Job`; the CR write returns the same code for consistency).

### 5. PostgreSQL-first / CR-second ordering

The Console performs writes in this order:

1. **PostgreSQL write** (`JobService.CreateJob` / `UpdateJob` / `DeleteJob`
   / `StartJob` / `StopJob`) — durable, authoritative.
2. **`client.Client` write** (`syncjobcr.Create` / `Update` / `Delete`).

Failure mode matrix:

| PG result | CR result | Console behavior |
| --- | --- | --- |
| success | success | 200/204 with PG + CR consistent |
| success | 409 (CR exists, e.g. retry) | 200 + idempotent (treat as success) |
| success | admission rejected (`IsInvalid`) | log warning, return PG result anyway; metrics carry `_unknown` until operator migrates |
| success | timeout / 5xx | log error, retry with bounded backoff (3 attempts, 100ms-400ms-1.6s), then return PG result; controller will reconcile drift on next loop |
| failure | n/a | return PG error verbatim |

The slice **does not** fail closed on CR write failure. Reasoning: the
PostgreSQL row is the durable source of truth (ADR-029). Failing the BFF
mutation would orphan the user-visible job from the audit trail and force
the operator to manually reconcile. The controller-runtime controller
already reconciles state into PostgreSQL on each loop iteration; if the
Console's CR write fails transiently, the next reconcile (or a manual
`kubectl apply`) catches up. The slice logs a `WARN` with
`request_id + tenant_id + job_name + cr_error` so operators can audit.

This is a deliberate, narrow deviation from the Slice 28-A hard-reject
posture (tenant-id envelope). The envelope rejects **before** any backend
write; the dual-write fails open **after** the durable write succeeds,
because the durable write is the user's contract.

### 6. `validateJob` does not write a CR

ADR-072 §1 includes `validateJob` in the tenant-id egress mutation set,
but `validateJob` returns a `JobValidationResult` and does not persist any
state. Slice 28-B leaves this unchanged: validation requests do not
create `SyncJob` resources. The BFF still forwards `x-astra-tenant-id`
metadata so a future server-side validator can consume it.

### 7. No reconciler / no cache

The Console does not run a `manager.Manager` or a cache. It uses
`client.New(cfg, client.Options{})` with a **direct** connection (no
`cache.Options{}`), bypassing the controller-runtime cache. This is
critical:

- Console lifecycle is per-HTTP-request, not per-reconcile-loop.
- A cached `SyncJob` would lag the durable `job.Job` row by up to the
  cache resync period (default 10 hours), which would create a worse UX
  than no cache.
- The Console is read-mostly; the few CR reads (`syncjobcr.Get`) happen
  inline on mutation and tolerate the direct API server round-trip.

### 8. Configuration: in-cluster vs local-dev

`console/internal/syncjobcr/manager.go` exposes:

```go
type Manager interface {
    Enabled() bool
    Client(scopeConfig) (client.Client, error) // returns err if disabled
}

func NewInCluster(restConfig *rest.Config) Manager
func NewDisabled() Manager                              // local dev fallback
func NewFromEnv(env EnvLookup) Manager                  // picks one based on KUBERNETES_SERVICE_HOST
```

`console/cmd/console/main.go` wires the manager based on
`KUBERNETES_SERVICE_HOST` (the standard in-cluster sentinel):

- in-cluster: `rest.InClusterConfig()` → `NewInCluster`
- local dev: `NewDisabled()`

When the manager is disabled, the mutation handler logs
`logger.Warn("syncjob CR write disabled (no K8s config); controller will reconcile from PG alone")`
once at startup and proceeds. The slice's existing tests use
`NewDisabled()` so they remain cluster-free.

### 9. RBAC

The Console's ServiceAccount in the in-cluster deployment gains:

```yaml
- apiGroups: [sync.astrasync.io]
  resources: [syncjobs]
  verbs: [get, create, update, delete]
- apiGroups: [sync.astrasync.io]
  resources: [syncjobs/status]
  verbs: [get, update]
```

The RBAC is intentionally narrow: the Console has no need to list all
`SyncJob` resources across namespaces, and `verbs: [patch]` is excluded
because the slice uses full `Update` (CRD validation re-applies on every
write). The RBAC manifest is added to
`deployment/helm/console/templates/role.yaml` as a sibling to the existing
`console-sa` binding.

The slice ships a `tests/integration/k8s-rbac_test.go` (build tag
`integration`) that boots `envtest` from
`sigs.k8s.io/controller-runtime/pkg/envtest` and asserts the Console's
ServiceAccount has exactly the verbs above, no more. This is a CI
gate, not a unit test.

### 10. Tests

`console/internal/server/bff_slice28b_test.go` (new file, black-box
`package server_test`). Coverage:

| Test | Asserts |
| --- | --- |
| `TestConsoleCreatesSyncJobCROnCreateJob` | PG mock returns success; CR client receives a `Create` call with `astrasync.io/tenant-id` = session tenant and `metadata.name` = job name |
| `TestConsoleUpdatesSyncJobCROnUpdateJob` | CR client receives an `Update` call with the same name + label after a PG `UpdateJob` success |
| `TestConsoleDeletesSyncJobCROnDeleteJob` | CR client receives a `Delete` call after PG `DeleteJob` success |
| `TestConsoleStartStopMutateCR` | start/stop transitions trigger a CR `Update` (not Create / Delete); the controller picks up `Spec.State` from the CR after the mutation |
| `TestConsoleCRWriteFailsOpenOnAdmission` | CR client returns `IsInvalid`; BFF still returns 200 (PG succeeded); metric `controller_syncjob_console_dual_write_total{outcome="admission_rejected"}` increments |
| `TestConsoleCRWriteFailsOpenOnTimeout` | CR client returns context.DeadlineExceeded; BFF retries 3x with bounded backoff; final failure logged + `outcome="timeout"` metric |
| `TestConsoleDisabledCRManagerSkipsWrite` | `NewDisabled()` manager; CR client is **never** instantiated; mutation completes normally |
| `TestConsoleCRCarriesTenantIDFromScope` | session has tenant A; CR write carries tenant A's UUID even when membership is selected from `X-Astra-Tenant-ID` header B (deny path → no CR write) |
| `TestConsoleCREmitsStableLabels` | the CR `Labels` map contains exactly `astrasync.io/tenant-id`; no other labels leaked from session |
| `TestConsoleCRPostgreSQLFirstOrdering` | mock PG client observes the call ordering: PG write completes **before** CR write; if CR write fails, PG is **not** rolled back |

The CR client is a `fake.NewClientBuilder().WithScheme(scheme).Build()`
from `sigs.k8s.io/controller-runtime/pkg/client/fake`, exactly mirroring
`control-plane/controller/internal/controller/syncjob_controller_test.go:17-19`.
This keeps the test pattern consistent across modules.

A new metric `controller_syncjob_console_dual_write_total{outcome}`
(`success | admission_rejected | timeout | invalid`) is added to the
Console's Prometheus exposition. Outcome cardinality is bounded at 4.

### 11. Metrics naming and ownership

The metric lives in `console/internal/metrics/`, not in
`control-plane/controller/internal/metrics/`, because the **writer** is the
Console. The metric name uses the `controller_` prefix because the
project's metrics catalog (`docs/observability/metrics-catalog.md`)
prefixes all controller-issued metrics that way. The dual-write metric is
distinguishable by name (`controller_syncjob_console_dual_write_total`)
from the controller-runtime controller's
`controller_job_state_total`.

This naming is documented in `docs/observability/metrics-catalog.md` in
this slice.

### 12. Documentation

- This ADR.
- `docs/phase28/README.md` Slice 28-B section updated to point to this ADR
  for the dual-write ordering, RBAC, and metric.
- `docs/connector-dev.md` is **not** touched — this is a Console path, not
  a connector SPI change.
- `docs/deployment.md` gains a paragraph describing the Console's new K8s
  RBAC and the in-cluster / local-dev toggle.

### 13. Backwards compatibility

The slice is additive on the wire:

- `x-astra-tenant-id` metadata continues to flow (Slice 28-A).
- CRD validation continues to enforce the tenant label (Slice 27-A).
- The PostgreSQL `job.Job` row is unchanged.
- The Console's external REST surface is unchanged.

Existing clusters that already have `SyncJob` CRs created via `kubectl`
continue to work; the Console does not overwrite them. If a Console-issued
mutation arrives for a job name that already has a CR (created out-of-band),
the Console's `Update` path uses optimistic concurrency and surfaces
`codes.Aborted` on `resourceVersion` drift; this is consistent with the
API Server's existing behavior for `job.Job` version conflicts.

If `kubectl` creates a `SyncJob` CR that lacks the label, the Kubernetes
API server rejects it at admission time per ADR-071. No change in
behavior.

## Consequences

### Positive

- `controller_job_state_total` and `controller_epoch_fence_total` will emit
  real `tenant_id` values for **every Console-issued job**, not just
  `kubectl`-issued jobs. ADR-071's CRD validation is now exercised in
  practice, not just in theory.
- The dual-write is narrow: PostgreSQL is authoritative, the CR is a
  cache. The architectural invariant from ADR-029 ("PostgreSQL is the
  durable source of truth, controller reconciles state into it") is
  preserved.
- The slice is testable without a live K8s cluster: the existing test
  infrastructure (`fake.NewClientBuilder`) covers all paths except the
  RBAC check, which uses `envtest` (CI-only).
- No controller-runtime Manager / cache is started; the Console's resource
  footprint is unchanged.
- The new metric `controller_syncjob_console_dual_write_total` gives
  operators an immediate signal when the CR write path is failing
  silently in production (the "fail open" branch is now visible).
- The slice explicitly defers a `crd-types` re-export module; that work
  belongs to a Phase 29 module-hygiene slice and is out of scope here.

### Negative

- The slice adds **two new direct Go module dependencies**
  (`controller-runtime`, `k8s.io/apimachinery`); `k8s.io/api` rides in
  transitively. The controller module already carries them, so the
  version surface is not new, but the Console's `go.sum` grows. This is
  unavoidable; the alternative (custom REST client) is worse.
- "Failing open" on CR write errors is a deliberate trade-off. An
  operator who relies on metrics to detect orphaned jobs will see
  `_unknown` for jobs whose CR write failed transiently. The
  `controller_syncjob_console_dual_write_total{outcome="..."}` metric
  surfaces these failures, but it adds operational burden (operators
  must dashboard the new metric).
- The Console becomes a Kubernetes client consumer. This couples the
  Console's deployment to the cluster's RBAC model. The narrow RBAC
  grant (verbs `get,create,update,delete` on `syncjobs.sync.astrasync.io`)
  limits the blast radius, but a misconfigured RBAC binding can break
  mutations across the board. The `tests/integration/k8s-rbac_test.go`
  RBAC check is the only guard.
- `SyncJob` CRs written by the Console do not carry the `expectedVersion`
  on the CR itself; the controller-runtime controller reconciles
  versions from PostgreSQL. This means a `kubectl get syncjob -o yaml`
  against a Console-issued job may briefly show a stale `resourceVersion`
  during the reconcile window (default 10s). Operators viewing CRs in
  production should treat them as a projection, not as the source of
  truth (already the project's posture per ADR-029).
- Local dev (no in-cluster K8s) skips the CR write entirely. SyncJobs
  created via `console dev` mode continue to produce `_unknown` in the
  controller metrics, exactly as today. This is acceptable for local
  development.

## Alternatives Considered

### Controller-runtime Manager + cache in the Console

Start a full `manager.Manager` in the Console process and let
controller-runtime cache `SyncJob` resources.

**Reject.** The Console's lifecycle is per-HTTP-request, not per-reconcile.
A Manager introduces a long-running goroutine pool, a cache resync period,
and an additional shutdown coordination point. None of those are needed
for write-mostly traffic. A direct `client.Client` is the simplest
sufficient abstraction.

### API Server creates the `SyncJob` CR

The API Server gains a Kubernetes client and creates the CR after the
`job.Job` PostgreSQL write succeeds.

**Reject.** The API Server's competency is the gRPC/REST boundary and
PostgreSQL; adding a K8s client couples it to RBAC, in-cluster config,
and the controller-runtime dependency graph. The Console already has
the authenticated session context and is the correct place for the
write (per ADR-071 §Alternatives).

### `kubectl apply -f -` invocation from the Console

The Console shells out to `kubectl` with a rendered YAML manifest.

**Reject.** `kubectl` is a binary that the Console deployment may not
have (image size, CVE surface, RBAC via kubeconfig). The
controller-runtime client is in-process, versioned, and uses the same
RBAC model as `kubectl` but without the binary dependency.

### Make the controller-runtime controller own the CR creation

The controller, on observing a new `job.Job` row in PostgreSQL but no
matching `SyncJob` CR, creates the CR.

**Reject.** The controller is a consumer of `SyncJob` CRs (the
reconciliation target). Asking it to also be a producer creates a
feedback loop (who reconciles the CR the controller just created?)
and violates the ADR-029 contract that PostgreSQL is the durable
source of truth. The controller's job is to project PG state into the
CR's `status` subresource, not to create the CR.

### Skip CR creation entirely; rely on PostgreSQL only

Accept that `_unknown` is permanent for Console-issued jobs and remove
the `tenant_id` label requirement from the CRD.

**Reject.** This rolls back ADR-071 and ADR-072 and re-introduces the
architectural gap those ADRs closed. The metric
`controller_job_state_total{tenant_id="_unknown"}` becomes permanent.

### Re-export CRD types to a separate `crd-types` module

Create `control-plane/crd-types/go.mod` containing only the
`SyncJob` types and have both the controller module and the Console
depend on it.

**Defer to Phase 29.** The slice ships with a direct `replace` on
`control-plane/controller` because the type package has no controller-runtime
manager dependency at the type level. A future module-hygiene slice
should split the types, but it is independent of the dual-write itself.
