# Phase 28: Console Mutation Tenant-Id Egress + SyncJob CR Wiring

## Status

Active (Slice 28-A shipped; Slice 28-B shipped, ADR-073 Accepted)

## Goal

Eliminate `_unknown` from `controller_job_state_total`,
`controller_epoch_fence_total`, and any future tenant-keyed metric that
crosses the Console → API Server boundary. The fix is incremental:

- **Slice 28-A**: BFF egress carries `x-astra-tenant-id` on every job
  mutation, with a hard reject when scope lacks tenant context. ADR-072
  (Accepted). Implementation: `tenantScopeCheck` + `backendContextWithTenant`
  in `console/internal/server/job_handlers.go`. Tests:
  `console/internal/server/bff_slice28_test.go` (10 tests passing).
- **Slice 28-B**: Console gains an in-cluster Kubernetes HTTP client and
  creates the `SyncJob` CR with `astrasync.io/tenant-id` after the
  durable PostgreSQL `job.Job` write succeeds. ADR-073 (Accepted).
  Implementation: `console/internal/syncjobcr` + `syncjobcr.NewDualWriter`
  wired into `console/cmd/console/main.go`. Tests:
  `console/internal/server/bff_slice28b_test.go` (11 tests passing) +
  `console/internal/syncjobcr/manager_test.go` (12 tests passing).

## Slices

### Slice 28-A.1 — `x-astra-tenant-id` outgoing metadata on every job mutation (Shipped)

The Console BFF (`console/internal/server/`) appends
`x-astra-tenant-id = <canonical-uuid>` to the outgoing gRPC metadata on:

- `POST /api/jobs`
- `PUT /api/jobs/{name}`
- `DELETE /api/jobs/{name}`
- `POST /api/jobs/{name}/start`
- `POST /api/jobs/{name}/stop`
- `POST /api/jobs/{name}/validate`

Read-only endpoints (any `GET /api/jobs/*`) do **not** carry the metadata.
The `Idempotency-Key` header → `IdempotencyKey` proto field contract
remains unchanged. Implementation:

- `tenantScopeCheck(scope)` defense-in-depth guard in
  `console/internal/server/job_handlers.go` — rejects mutations with
  `codes.PermissionDenied("tenant scope denied")` when `scope.tenantID`
  is empty. Runs **after** `s.scope(request)` and **before** any
  `s.mutations.*` call.
- `Server.backendContextWithTenant(request, session, timeout, tenantID)` in
  `console/internal/server/server.go` line 515 — wraps `backendContext`
  and appends `x-astra-tenant-id` when `tenantID != ""`. The original
  `backendContext` is retained for read-only paths.
- All six mutation handlers replaced `s.backendContext(...)` with
  `s.backendContextWithTenant(..., scope.tenantID)`.

### Slice 28-A.2 — Tests (`bff_slice28_test.go`) (Shipped)

File: `console/internal/server/bff_slice28_test.go`. Black-box
(`package server_test`) per project testing rule. Coverage:

| Test | Path | Expected backend metadata |
| --- | --- | --- |
| `TestConsoleForwardTenantIDOnCreateJob` | `POST /api/jobs` | `x-astra-tenant-id` = session tenant |
| `TestConsoleForwardTenantIDOnUpdateJob` | `PUT /api/jobs/{name}` | `x-astra-tenant-id` = session tenant |
| `TestConsoleForwardTenantIDOnDeleteJob` | `DELETE /api/jobs/{name}` | `x-astra-tenant-id` = session tenant |
| `TestConsoleForwardTenantIDOnStartStopJob` | `POST /api/jobs/{name}/start\|stop` | `x-astra-tenant-id` = session tenant |
| `TestConsoleForwardTenantIDOnValidateJob` | `POST /api/jobs/{name}/validate` | `x-astra-tenant-id` = session tenant |
| `TestConsoleRejectsMutationWhenTenantIDEmpty` | All six paths | `PermissionDenied`; backend never called |
| `TestConsoleRejectsMutationWhenScopeMismatch` | `PUT` with `X-Astra-Tenant-ID` ≠ session tenant | `Forbidden`; backend never called |
| `TestConsoleReadOnlyEndpointsDoNotForwardTenantID` | `GET /api/jobs`, `GET /api/jobs/{name}` | No `x-astra-tenant-id` metadata |
| `TestConsoleMutationIdempotencyKeyContinuity` | `POST /api/jobs` with valid `Idempotency-Key` | `request.IdempotencyKey` matches header (regression guard) |
| `TestConsoleMutationRejectsInvalidCSRFAndOrigin` | mutation path with bad CSRF / origin | 403; backend never called |
| `TestPermissionDeniedStatusMapsTo403` | `codes.PermissionDenied` translation | status code surfaces as 403 |

Each test installs a `jobMutationBackend` (in-memory `fakeBFFBackend`
subclass) that captures `metadata.FromOutgoingContext` and asserts on
the `x-astra-tenant-id` payload plus call counters per mutation kind.

### Slice 28-A.3 — Documentation, observability, CHANGELOG (Shipped)

- ADR-072 (Accepted) — `docs/adr/adr-072-phase28-console-tenant-id-egress.md`.
- This README.
- `CHANGELOG.md` "Unreleased / Added" entry:
  "Console BFF forwards `x-astra-tenant-id` on every job mutation; rejects
  mutation when scope lacks tenant-id (Phase 28 Slice 28-A, ADR-072)".
- No new dashboard or alert — the metric is consumed by a future Phase 29
  API Server interceptor. Slice 28-A is purely the egress contract.

### Slice 28-B — Console dual-writes `SyncJob` CR via in-cluster K8s client (Shipped, ADR-073)

ADR-073 (Accepted) closes the third gap: the Console writes the `job.Job`
PostgreSQL row and then creates / updates / deletes the corresponding
`SyncJob` CR via an in-cluster Kubernetes HTTP client. The
controller-runtime controller
(`control-plane/controller/internal/controller/syncjob_controller.go`)
now observes Console-issued jobs and `controller_job_state_total{tenant_id="..."}`
emits a real tenant UUID instead of `_unknown`.

The implementation chose **plain `net/http` against the Kubernetes API
server** (ServiceAccount token + CA cert) instead of pulling in
`sigs.k8s.io/controller-runtime` directly. Rationale: the controller-
runtime dependency chain (`k8s.io/apiserver`, `k8s.io/component-base`,
`k8s.io/streaming`) was not consistently available in the module cache,
and the HTTP client is functionally equivalent for the four mutations
the Console needs. The CRD types (`SyncJob`, `SyncJobSpec`,
`TenantLabelKey`) are mirrored locally in `console/internal/syncjobcr`
to keep the Console's dependency surface small.

PostgreSQL-first / CR-second ordering. CR write failures **fail open**
(logged + metric) because the durable PG row is the user's contract per
ADR-029. The new metric `controller_syncjob_console_dual_write_total{mutation,outcome}`
is bounded to 3 mutations × 5 outcomes = 15 series and surfaces the
fail-open branch to operators.

Concrete implementation surface:

| Component | File | Notes |
| --- | --- | --- |
| `syncjobcr.Manager` | `console/internal/syncjobcr/manager.go` | In-cluster / disabled toggle; reads `/var/run/secrets/kubernetes.io/serviceaccount/{token,ca.crt}` when `KUBERNETES_SERVICE_HOST` is set |
| `syncjobcr.DualWriter` | `console/internal/syncjobcr/manager.go` | Create / Update / Delete; bounded retry (3 attempts at 100ms / 400ms / 1.6s) |
| `Recorder.RecordDualWrite` | `console/observability/metrics.go` | Bounded Prometheus counter; cardinality 15 |
| `server.Config.CRWriter` | `console/internal/server/server.go` | `any`-typed injection; nil → no-op writer |
| `Server.crWriter` | `console/internal/server/server.go` | `crWriter` interface derived from `Config.CRWriter` |
| `jobSpecToCR` | `console/internal/server/job_handlers.go` | Marshals `*jobv1.JobSpec` into `*syncjobcr.SyncJobSpec` |
| Mutation handlers | `console/internal/server/job_handlers.go` | `createJob` / `updateJob` / `deleteJob` call `WriteCR` after the PG write succeeds; `startJob` / `stopJob` deliberately do **not** write the CR (ADR-073 §5) |
| Wiring | `console/cmd/console/main.go` | Builds `syncjobcr.NewDualWriter` from `NewFromEnv(os.Getenv)` and forwards into `server.NewWithConfig` |
| Tests | `console/internal/server/bff_slice28b_test.go` | 11 tests covering happy path, admission rejection, timeout retry, scope mismatch, label stability, PG-first ordering, and disabled-manager bypass |
| Unit tests | `console/internal/syncjobcr/manager_test.go` | Manager / DualWriter unit tests using `httptest.NewTLSServer` (no live cluster required) |

Narrow RBAC grant on the Console's ServiceAccount (live in
`deployment/helm/console/templates/role.yaml`):

```yaml
- apiGroups: [sync.astrasync.io]
  resources: [syncjobs]
  verbs: [get, create, update, delete]
- apiGroups: [sync.astrasync.io]
  resources: [syncjobs/status]
  verbs: [get, update]
```

`verbs: [patch]` is intentionally excluded; the slice uses full
`client.Update` (CRD validation re-applies on every write).

## Non-Goals (this phase)

- The API Server **does not** consume `x-astra-tenant-id` yet. Phase 29
  work.
- No `kubectl` plugin or local CLI integration. SyncJob CRs created via
  `kubectl` still need an operator-side label patch per the ADR-071
  migration note.
- Console-side namespace rotation. The BFF's `X-Astra-Tenant-ID` header
  continues to drive scope selection.

## Acceptance criteria

### Slice 28-A (Shipped)

- [x] All ten tests in `bff_slice28_test.go` pass on
  `go test ./console/internal/server/...`.
- [x] `go vet ./console/...` clean.
- [x] No new dependencies in `console/go.mod`.
- [x] `CHANGELOG.md` "Unreleased / Added" includes the entry.

### Slice 28-B (Shipped)

- [x] All eleven tests in `bff_slice28b_test.go` pass on
  `go test ./console/internal/server/...`.
- [x] All twelve tests in `console/internal/syncjobcr/manager_test.go` pass.
- [x] `controller_syncjob_console_dual_write_total` registered in
  `console/observability/metrics.go` with cardinality bounded to 15
  (3 mutations × 5 outcomes).
- [x] No new direct Go module dependencies (chose `net/http` over
  controller-runtime to avoid `k8s.io/apiserver` chain).
- [x] Console ServiceAccount RBAC grant added in
  `deployment/helm/console/templates/role.yaml` (see ADR-073 §9).
- [ ] `tests/integration/k8s-rbac_test.go` (envtest-based) — deferred
  until Phase 29 alongside the rest of the envtest infrastructure.
- [ ] `docs/deployment.md` paragraph describing the new K8s RBAC and
  the in-cluster / local-dev toggle — deferred to Phase 29.
- [ ] `docs/observability/metrics-catalog.md` row for the new metric —
  deferred to Phase 29.

## Rollback

### Slice 28-A rollback

Pure additive change. Reverting the commit restores the previous behaviour
(no metadata, no scope check). The slice is sized for a single revert if
the server-side consumer disagrees with the metadata convention.

### Slice 28-B rollback

Slightly larger: the Console's `go.mod` gains three dependencies and the
ServiceAccount gains an RBAC binding. Reverting the slice restores the
Slice 28-A state (BFF egress only, no CR write). The CRD validation
from Slice 27-A is unaffected by either direction.
