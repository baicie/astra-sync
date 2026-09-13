# Changelog

All notable changes to this project are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Phase 36 (ADR-085): controller integration tests now use
  controller-runtime envtest against the generated SyncJob CRD. The
  new `internal/controller` integration suite validates structural CRD
  admission, status subresource isolation, `resourceVersion` conflicts, and
  finalizer blocking in the standalone `control-plane/controller`
  module. The shared integration workflow installs pinned
  `setup-envtest v0.24.1`, exports `KUBEBUILDER_ASSETS`, and runs the
  controller suite from the correct module root. Envtest also proved
  that ADR-071's metadata-label CEL rule was not installable; ADR-086
  removes the invalid rule and tracks admission enforcement separately.

- Phase 37 (ADR-087): add a Kubernetes `ValidatingAdmissionPolicy` and
  binding for SyncJob `astrasync.io/tenant-id` admission. The policy
  denies `CREATE` and `UPDATE` requests with missing or non-canonical
  tenant labels. Controller envtest coverage now installs the policy
  and verifies its deny and accept paths.

- Phase 38 (ADR-088): make the SyncJob CRD and tenant-label admission
  policy declarative ArgoCD prerequisites. A dedicated Kustomize bundle
  and single-cluster/multi-cluster Applications install the
  cluster-scoped resources with prune disabled and self-heal enabled.

- Phase 29 (ADR-074): the API Server now consumes
  `x-astra-tenant-id` incoming gRPC metadata on every mutating
  RPC. The authn interceptor (Phase 29 §3) extracts the
  metadata, validates it is a canonical UUID, and reconciles
  it against the principal's active membership. A mismatch is
  rejected with `PermissionDenied` and audited as
  `TENANT_DENIED` / `TENANT_ENVELOPE_INVALID`. The verified
  tenant-id is attached to the request context via
  `authn.WithJobTenantID` and consumed by `JobService` so the
  `Mutation.TenantID` is the value the API Server authorised
  the request for. Migration
  `control-plane/job/postgres/migrations/003_jobs_tenant_id.sql`
  adds the nullable `tenant_id UUID` column to
  `astrasync_control_jobs`; the atomic mutation path writes
  the verified tenant-id on every INSERT. Tests in
  `authn/tenant_metadata_test.go` (5) and the new
  `interceptor_test.go` (3) and `job_mutation_service_test.go`
  (2) cases cover the contract.

- `console/internal/server/job_handlers.go`: Phase 28 slice 28-A
  (ADR-072) adds `x-astra-tenant-id` outgoing gRPC metadata on every
  job mutation (`POST/PUT/DELETE /api/jobs`, `/api/jobs/{name}/start`,
  `/api/jobs/{name}/stop`, `/api/jobs/{name}/validate`). The
  BFF now fails-closed when the resolved scope has an empty tenant-id
  before forwarding. Read-only endpoints are unchanged.
  `console/internal/server/bff_slice28_test.go` covers the egress
  contract; `console/internal/server/protojson_test.go` documents the
  exact protojson enum value names for job specs.

- ADR-073 (Accepted): Phase 28 slice 28-B — Console learns the
  controller-runtime client and creates the `SyncJob` CR after the
  durable PostgreSQL `job.Job` write succeeds. Closes the last gap
  before `controller_job_state_total{tenant_id}` becomes real for
  Console-issued jobs. Implementation lands in Phase 29 once the ADR
  is reviewed.

- Phase 30 (ADR-075): Console tenant-id envelope chain regression
  test (`console/internal/server/chain_e2e_test.go`, 3 cases) pins
  the join between the BFF egress (Phase 28-A) and the Console
  `SyncJob` CR dual-write (Phase 28-B). The chain test asserts
  that the same verified `tenantID` reaches both (a) the api-server
  outgoing gRPC metadata captured by the fake backend and (b) the
  `syncjobcr.WriteInput.Scope.TenantID` captured by the in-memory
  CR writer recorder, in the same HTTP request. A regression that
  swaps `scope.tenantID` for `session.Principal.ID` (or any other
  identity source) is caught by `chain[cr-write]` mismatch at
  unit-test speed. The controller-side half of the chain
  (`controller_job_state_total{tenant_id}` emission from the
  label) continues to be pinned by
  `control-plane/controller/internal/controller/syncjob_emission_test.go`

- Phase 31 (ADR-076 / ADR-079 / ADR-080): the cross-module chain
  test fixture for the tenant-id envelope ships at
  `tests/cross-module/chain-tenant-id/` with its own
  `go.mod` (replace-directive against `console` and
  `control-plane/api-server`). Five cases drive the
  `console` BFF via the public `console.NewWithDevelopmentSession`
  façade (ADR-080 §1) against a test fake implementing
  `bffbackend.Backend`, asserting on (a) the BFF egress
  `x-astra-tenant-id` (ADR-072 / ADR-074 §3), (b) the
  BFF ingress contract — malformed / mismatched
  `X-Astra-Tenant-ID` is rejected before reaching the
  api-server, and (c) the migration file
  `003_jobs_tenant_id.sql` declares the `tenant_id UUID` column
  and its index. ADR-080 documents the Go `internal/` rule
  that prevented the originally planned direct import of
  the api-server interceptor; the cross-module fixture
  consequently drives the public surface only, and the
  interceptor boundary is covered by Layer 1
  (`control-plane/api-server/internal/authn/interceptor_test.go`).
  A new CI workflow
  `.github/workflows/cross-module-chain-tenant-id.yml`
  runs the fixture on PRs that touch `console/`,
  `control-plane/api-server/`, `control-plane/job/`,
  `docs/phase31/`, or `tests/cross-module/chain-tenant-id/`.
  Phase 31 is test-only: no production code, no schema
  migration, no metric, no RBAC, no helm change.

- Phase 32 (ADR-081): tenant-id label-translation Layer-1 test
  ships at
  `console/internal/syncjobcr/manager_label_translation_test.go`
  with one happy-path case (canonical UUID is written verbatim
  into `metadata.labels["astrasync.io/tenant-id"]`) and one
  table-driven rejection case covering five non-canonical UUID
  forms (uppercase, brace, `urn:uuid:` prefix, whitespace
  padding, empty). All six cases pass on
  `go test ./console/internal/syncjobcr/... -count=1`.
  `realDualWriter.create` gains a single
  `IsCanonicalTenantID` guard (4 lines) so the rejection cases
  short-circuit locally and emit `controller_syncjob_console_dual_write_total{outcome="invalid"}`
  without contacting the K8s API server. The BFF ingress
  canonical-UUID check (Phase 28-A / Phase 29) and the K8s CEL
  `XValidation` rule on `astrasync.io/tenant-id` (ADR-071 §2)
  remain the upstream and downstream lines of defence. The
  `update`-path guard is deferred to Phase 33 (ADR-081
  §Follow-ups). Phase 32 is test-only plus the minimum
  production-code change required to make the rejection cases
  short-circuit locally.

- Phase 33 (ADR-082): tenant-id label-translation Layer-1 test
  is extended to the update mutation. Three new cases ship at
  `console/internal/syncjobcr/manager_label_translation_test.go`:
  `TestLabelTranslationUpdatePreservesCanonicalUUID` (happy
  path on update — the PUT body's
  `metadata.labels["astrasync.io/tenant-id"]` is verbatim and
  replaces the stale label from the existing CR),
  `TestLabelTranslationUpdateRejectsNonCanonicalTenantIDs`
  (table-driven, five non-canonical UUID forms short-circuit
  to `OutcomeInvalid` with zero server hits), and
  `TestLabelTranslationUpdateGuardShortCircuitsBeforeGET`
  (the guard fires before the discovery GET — neither GET nor
  PUT reaches the API server on malformed input).
  `realDualWriter.update` gains a single
  `IsCanonicalTenantID` guard (4 lines) mirroring the create-
  path guard. The Phase 32 metric diagnostic split
  (`invalid` = writer refused; `admission_rejected` = apiserver
  refused at CEL validation) is now symmetric across create
  and update. All twelve label-translation cases pass on
  `go test ./console/internal/syncjobcr/... -count=1`. Phase 33
  is test-only plus the minimum production-code change
  required to make the rejection cases short-circuit locally.
  (ADR-066 §3, ADR-069 §Slice 51.1). Phase 30 ships only test code;
  no production code, no new metric, no new dependency, no helm
  resource change.

- Phase 31 (ADR-076, Proposed): cross-module tenant-id envelope
  chain test fixture (`tests/cross-module/chain-tenant-id/`)
  extends the regression surface from single-module chain
  (Phase 30) to cross-module chain — pinning the join between the
  Console BFF, the API Server interceptor, the mutation
  repository, and the PostgreSQL `astrasync_control_jobs.
  tenant_id` column in a single test binary. Five cases (full
  chain match, BFF-vs-membership mismatch rejection, malformed
  metadata rejection, no-metadata fallback, migration `003_jobs_
  tenant_id.sql` persistence) are exercised in-process via a
  real `*grpc.Server` + `authn.Interceptor` + in-memory
  `Memory.Repository` and a BFF client adapter that invokes the
  same handler code path the HTTP server invokes. The new
  module's `go.mod` uses `replace` directives to import `console`
  and `api-server` without forcing either module to take on a
  dependency on the other. Phase 31 ships only test code; no
  production change, no schema migration, no metric, no RBAC
  change. New CI workflow
  `.github/workflows/cross-module-chain-tenant-id.yml` runs the
  five cases on PR / push-to-main-or-develop, with path-filtered
  triggers so unrelated PRs do not pay the ~5s cost.

- Phase 27 / 28 / 29 audit backfill (ADR-077, Proposed): the
  branch `phase13/kubernetes-production-hardening` previously
  landed design ADRs (ADR-071 / 072 / 073 / 074) and Phase
  READMEs (`docs/phase28/README.md`, `docs/phase29/README.md`)
  on disk but without committing them, and likewise held the
  corresponding production code (api-server tenant metadata,
  api-server interceptor, console BFF egress + dual-write,
  controller CRD types, postgres migration `003_jobs_tenant_id`,
  helm syncjob-cr-role + console tenant scope) as untracked or
  modified-but-uncommitted. Phase 30 / 31 tests passed locally
  because the working tree held the implementation; a fresh clone
  of the branch tip would not. This audit ADR records the
  discrepancy and the eight-commit per-slice backfill strategy,
  restoring branch tip integrity without squashing the existing
  Phase 30 / 31 history.

- Phase 27 / 29 audit clarification (ADR-078, Proposed):
  ADR-077 §Context incorrectly classified six files
  (`interceptor_test.go`, `job_mutation_service_test.go`,
  `job_validation_service.go`, `mutation_repository.go`,
  `repository.go`, `sync.astrasync.io_syncjobs.yaml`) as
  "phantom modified". The audit methodology used a single
  `git ls-tree` comparison, which only confirms HEAD-vs-origin-
  main blob equality and does not detect uncommitted working-
  tree deltas. Inspection of `git diff` shows the six files
  have substantive +287 / +21 / -10 additions tied to
  ADR-071 / ADR-074. Three follow-up commits re-categorize
  these as Phase 27 / 29 work and append them to the
  per-slice backfill history; ADR-078 documents the corrected
  audit methodology (`git diff` step required) for future
  audits.

- Phase 31 implementation corrections (ADR-079, Proposed):
  Pre-implementation audit of ADR-076 §2.3, §4.2, §7 against
  the actual `control-plane/job`, `control-plane/api-server/
  internal/authn`, and `job_mutation_service_test.go` surfaces
  revealed three corrections before Phase 31 cross-module
  fixture implementation begins. (1) `jobmemory.Repository`
  does **not** implement `MutationRepository`; the fixture
  must adopt the `recordingJobMutationRepository` pattern
  (embed `job.Repository` interface, override `ApplyMutation` /
  `ReplayMutation`). (2) Metadata key is `x-astra-tenant-id`
  per the `authn.TenantMetadataKey` constant; the fixture
  imports the constant rather than a string literal. (3)
  Migration test default becomes SQL parse + insert-path
  assertion (not testcontainers); the in-memory adapter for
  migration verification does not exist, so dry-run parse is
  the only viable option in this slice.

- Phase 31 cross-module public-surface constraint (ADR-080,
  Proposed): The Phase 31 fixture's first draft assumed it
  could import `control-plane/api-server/internal/authn` and
  `control-plane/api-server/internal/service` directly. Go's
  `internal/` package rule forbids importing those packages
  from a sibling module. ADR-076 §2 and ADR-079 §1 inherit this
  assumption. The corrected fixture uses only the public
  surface: `console` package + `control-plane/api-server/gen/
  go/v1.JobServiceServer` (with `UnimplementedJobServiceServer`
  embedded). The api-server interceptor's ordering rule
  continues to be pinned by `interceptor_test.go` (Layer 1).
  The five test cases are recast accordingly: mismatch /
  malformed / fallback cases become BFF ingress contract
  tests, and the migration case becomes a SQL parse +
  documentation test. ADR-080 supersedes ADR-076 §2 and
  ADR-079 §1 / §4 by reference.

- Phase 34: tenant-id binding for the controller
  reconcile-level metric. The
  `controller_job_controller_reconcile_duration_seconds{tenant_id}`
  series was historically a single degenerate bucket bound
  to the hard-coded `"_unknown"` label, because the
  `Reconcile` defer in
  `control-plane/controller/internal/controller/syncjob_controller.go`
  ignored the SyncJob CR's `astrasync.io/tenant-id` label.
  Phase 34 derives the label from
  `resource.Labels["astrasync.io/tenant-id"]` after the K8s
  `Get` succeeds, routing it through
  `observability/normalize.NormalizeTenant` (the same
  allowlist every other tenant-deriving Recorder in the
  control plane uses, ADR-058 §3). The pre-`Get` placeholder
  stays at `_unknown` so a `Get` failure is still
  attributed to the unknown bucket rather than leaking a
  stale value. Five new Layer-1 cases in
  `reconcile_tenant_id_metric_test.go` cover canonical UUID,
  missing label, non-canonical UUID, `_platform` self-scope,
  and empty string. The historical
  `TestReconcileObservesSuccessAndFailureOutcomes` is
  refreshed to assert against the canonical tenant
  (the resource is fetched before the Jobs-nil guard fires,
  so the failure outcome is also bound to the canonical
  tenant). All tests pass on
  `go test ./control-plane/controller/... -count=1`
  (10.0s). The change unblocks the envtest-backed
  controller reconcile regression (ADR-082 §Follow-ups) —
  the unit-tested contract is now the metric binding the
  dashboard recipes expect, so envtest only needs to verify
  the Get → finalizer-add → ProjectStatus flow without
  re-pinning label semantics. Phase 34 is test-only plus
  the minimum production-code change required to make the
  metric bind the right tenant.

- Phase 35 (ADR-084): migrates the two pre-existing PostgreSQL
  integration tests under `control-plane/job/postgres/` from
  an external PostgreSQL (`ASTRASYNC_TEST_POSTGRES_URL`) to a
  hermetic testcontainers-go instance. Four artefacts ship:
  `//go:build integration` build tag on both
  `repository_integration_test.go` and
  `mutation_integration_test.go` (fixing the missing build-tag
  compliance violation from testing.mdc §2), removal of the
  `t.Skip` short-circuits (fixing the `t.Skip` on invariant
  tests violation from testing.mdc §8), a shared helper
  (`postgres_testcontainer_helper_test.go`) that boots
  `postgres:16-alpine` and applies `*.sql` migrations, and a
  new CI lane (`.github/workflows/control-plane-integration.yml`)
  that runs `go test -tags=integration` on PRs touching the
  persistence layer. The helper is a single `startPostgresContainer(t)`
  function exported from `package postgres_test`; both integration
  tests call it in place of the removed `t.Skip` guard.
  `github.com/testcontainers/testcontainers-go v0.35.0` and the
  postgres module are added to `control-plane/go.mod` (the root
  module that owns `control-plane/job/`). ADR-083 (shared
  helper module + three new consumer tests) is superseded by
  ADR-084 (migration of existing tests only); ADR-083 §Decision
  assumed the SQL-level coverage did not exist, but
  `mutation_integration_test.go` already covered the cross-module
  atomic-job-mutation path with a real PostgreSQL. Phase 35 is
  test-only plus CI and dependency changes; no production code.

### Fixed

- Align every Maven child module parent version with the root
  `0.8.0` reactor version. The repository could not resolve its
  parent POMs after the release version bump.
- Fix the CLI descriptor output to map protobuf enums to names
  before passing them to `String.join`, restoring Java compilation.
- Make `make catalog-export` write the deployment catalog by
  default and honor `CATALOG_OUTPUT`, matching the documented
  re-bake workflow.

<!-- Add new Phase content above this line. -->

## [v0.8.0] - 2026-09-08

Phase 24 (API Server session-revoke emission, ADR-068) +
Phase 25 (controller epoch-fence emission, ADR-069) +
Phase 26 (catalog closeout + reconcile loop tests). Completes
the emission half of the Phase 17 observability activation
matrix for the last two remaining metrics
(`apiserver_session_revoke_total`, `controller_epoch_fence_total`)
and freezes the Phase 17 backlog table. See ADR-070 for the
release-cut rationale.

### Added

- `control-plane/api-server/internal/service/access_service.go`:
  Phase 24 slice 50.2 (ADR-068 §2) adds the
  `RevokeConsoleSession` gRPC handler. Every revoke RPC now
  emits `apiserver_session_revoke_total` via the metrics
  Recorder: once per unique active tenant the target principal
  holds a membership in. `outcome` is `success` if the revoke
  deletes one or more sessions, `noop` if there are none to
  revoke. `tenant_id` is the per-active-tenant label. The
  handler enforces platform-admin (gRPC `PermissionDenied`
  otherwise), validates the principal ID (gRPC `InvalidArgument`
  on parse failure), and emits a single
  `access.console_session.revoked` audit event in the same
  serializable transaction as the session DELETE.

- `control-plane/auth/access_repository.go`,
  `control-plane/auth/postgres/repository.go`: Phase 24 slice
  50.2 adds `AccessRepository.RevokeConsoleSessionsForPrincipal`.
  The Postgres implementation wraps the session DELETE and the
  audit-event INSERT in a single `sql.LevelSerializable`
  transaction; either both rows are committed or both roll back.

- `control-plane/api-server/cmd/server/main.go`: Phase 24 slice
  50.3 wires the existing `metricRecorder` into the access
  service via the new `service.WithAccessRevokeRecorder`
  functional option. No new wiring or initialization required.

- `control-plane/controller/internal/controller/syncjob_controller.go`:
  Phase 25 slice 51.1 (ADR-069 §2) adds the `observeEpochFence`
  helper and calls it after every successful `r.Jobs.Update`
  in the three converge sites: spec-change stop,
  desired-state transition, and deletion. `outcome` is `fenced`
  when the next epoch exceeds the stored epoch, `success` when
  the epoch is unchanged, `failure` for a lower epoch. The
  Recorder is nil-safe.

- `control-plane/controller/internal/controller/syncjob_controller_test.go`:
  Phase 26 slice 53.0 (ADR-069 follow-up) adds 18 table-driven
  unit tests covering the reconcile loop's happy + rejection paths.
  Coverage spans: finalizer adoption (`adds_finalizer_when_absent`),
  deletion paths (`deletion_removes_finalizer_when_job_not_found`,
  `deletion_stops_active_job_and_requeues`,
  `deletion_deletes_inactive_job_and_removes_finalizer`,
  `deletion_requeues_on_delete_conflict`), converge state
  transitions (`converge_starts_stopped_job`,
  `converge_stops_running_job`, `converge_noop_returns_requeue`,
  `converge_requeues_on_persistent_conflict`,
  `converge_replaces_spec_when_inactive`), invariants
  (`converge_spec_change_while_active_requests_stop_first`,
  `ignores_spec_change_while_canceling`,
  `converge_stops_inactive_job_via_desired_stop`), race +
  error handling (`converge_creates_job_when_not_found`,
  `converge_retries_on_create_already_exists`,
  `converge_error_from_jobs_get_passes_through`), and guards
  (`returns_error_when_jobs_repository_is_nil`,
  `returns_nil_for_unknown_resource`). The test file provides a
  `testSyncJob` helper that the pre-existing
  `observability_test.go` already references (closing pre-existing
  test debt) and a `fakeJobsRepository` wrapper that injects
  per-call overrides for transient conditions without touching the
  production repository logic. `WithStatusSubresource` is enabled
  on the fake K8s client so `projectStatus` updates do not race with
  the controller-runtime cache.

### Removed

- `scripts/reorder-changelog.py`: Phase 26 slice 53.2 removes the
  broken script. The script was destructive: when run against the
  current CHANGELOG.md, it overwrote the file with an empty
  `[Unreleased]` placeholder and dropped every other release
  section. The Phase 26 closeout applies the date stamps to all
  `[vX.Y.Z]` section headers manually instead.

<!-- Add new Phase content above this line. -->


## [v0.7.0] - 2026-09-08

### Added

- `control-plane/console/internal/authflow/manager.go`: Phase 17
  slice 43.1.5 (ADR-058 §2) wires `authmetrics.Recorder`
  into Console BFF `Manager.CompleteLogin`. Every sign-in outcome
  now emits `auth_sign_in_total` via `Recorder.ObserveSignIn`:
  DENIED paths (store/consume/exchange/validation errors) emit
  `outcome=rejected`; session-creation failure emits
  `outcome=failure`; success emits `outcome=success`.  `tenant_id`
  is the first key in `principal.Memberships`, or `_platform` for
  DENIED paths / principals with no memberships. This completes
  the Phase 17 activation matrix: the `auth_sign_in_total` and
  `apiserver_sign_in_total` families move from "Recorder wired" to
  "emitted". The Recorder is nil-safe; callers without a
  Recorder are unaffected.

- `control-plane/auth/metrics.go`: Phase 17 slice 43.1.5 adds a
  public re-export of `authmetrics.Recorder` so callers outside
  the auth module (Console BFF, API Server) can construct and
  inject the Recorder via functional options without violating
  the `auth/internal/authmetrics` package boundary.

- `control-plane/console/internal/authflow/manager.go` (refactor):
  `Manager.provider` changes from `*oidc.Client` (concrete type) to
  `oidcProvider` (unexported interface) to enable test injection
  without a real OIDC discovery endpoint. The real `*oidc.Client`
  satisfies the interface; all existing callers are unaffected.

- `docs/observability/metrics-catalog.md`: Phase 17 slice 43.1.5
  updates `apiserver_sign_in_total` and `auth_sign_in_total` rows
  from "Recorder wired" to "emitted" with the full outcome/tenant
  derivation contract.

- `docs/phase17/README.md`: Phase 17 README updated to reflect
  slice 43.1.5 Done status and revised backlog snapshot as of
  2026-09-08.

- `control-plane/controller/internal/controller/syncjob_controller.go`:
  Phase 23 slice 49 (ADR-066) wires `controller_job_state_total` at the
  reconcile-loop durable commit boundary. The `SyncJobReconciler` struct
  gains a `Recorder *metrics.Recorder` field; `SetupWithManager(manager,
  recorder)` accepts the recorder. A new `observeTransition(resource,
  stored, next)` helper derives `tenant_id` from the SyncJob resource
  label `astrasync.io/tenant-id` (collapsing to `_unknown` if absent)
  and calls `Recorder.ObserveStateTransition` when
  `stored.Status.State != next.Status.State`. The helper is wired at four
  durable-commit points: two in `converge` (spec-change stop path +
  desired-state transition path) and one in `reconcileDeletion` (active
  state stop path). The call site is post-`r.Jobs.Update(...)` returning
  nil — the moment the job repository has accepted the new state. The
  Recorder method is nil-safe, so callers that construct a Reconciler
  without a Recorder are unaffected.

- `control-plane/controller/cmd/controller/main.go`: `SetupWithManager`
  is called with the existing `controllerMetrics` recorder, closing the
  injection path between the controller-runtime registerer and the
  reconcile loop.

- `control-plane/controller/api/v1/syncjob_types.go`: godoc on the
  `SyncJob` type documents the `astrasync.io/tenant-id` label
  requirement for observability. The label is not enforced by Kubernetes
  itself; a future slice (49.1.5) adds kubebuilder validation and the
  API server wiring.

- `control-plane/controller/internal/controller/syncjob_emission_test.go`:
  Phase 23 slice 49 tests cover `observeTransition` happy path
  (`INITIALIZING → RUNNING`), missing-label collapse to `_unknown`,
  non-canonical tenant collapse, and `_platform` self-scope. Two
  negative tests verify nil-safety and the no-op when state is unchanged
  (the durable-commit contract documented in ADR-058 §2).

- `docs/observability/metrics-catalog.md`: Phase 23 slice 49 updates
  the `controller_job_state_total` row from "Recorder wired in Phase 17
  slice 43.3; reconcile-path wiring pending" to the full emission
  description with the durable-commit boundary, K8s label derivation,
  and nil-safety contract documented. The `controller_epoch_fence_total`
  row remains Recorder-wired only (slice 49.3.5 is the Phase 23+
  candidate once ADR-053 §3 settles the durable commit decision for
  fence responses).

- `docs/adr/adr-066-phase23-controller-reconcile-emission.md`:
  Phase 23 umbrella decision. Records scope (slice 49.1 / 49.2 / 49.3 /
  49.4: `controller_job_state_total` emission via reconcile boundary),
  the durable-commit emission pattern, the K8s-label tenant derivation,
  non-goals (slices 49.1.5 / 49.3.5 / 43.1.5 / 26.F9), and the
  ADR-053 / ADR-029 dependency chain.

### Added

- `control-plane/auth/postgres/repository.go`: Phase 22 slice 48.1
  (ADR-065) adds `LoadTenantIDsForPrincipal(ctx, principalID)` which
  returns the unique active tenant IDs for a principal via a
  `SELECT DISTINCT tenant_id FROM astrasync_auth_memberships WHERE
  principal_id = $1 AND status = 'ACTIVE'` query. The method is used
  by `RevokeSessionsForPrincipal` to derive the `tenant_id` label for
  `auth_session_revoke_total` emission. `RevokeSessionsForPrincipal`
  now returns `(int64, []string, error)` — the session count and the
  list of active tenant IDs — in a serializable transaction so both
  values are from a consistent snapshot.

- `control-plane/auth/cmd/admin/main.go`: Phase 22 slice 48.2
  (ADR-065) wires `authmetrics.Recorder` into the admin CLI
  `revoke-session` operation. For `opRevokeSession`, a
  `prometheus.NewRegistry()` is created and
  `authmetrics.NewRecorder(registry)` is registered and stored on
  `adminCommand`. After `RevokeSessionsForPrincipal` succeeds, the
  command calls `recorder.ObserveSessionRevoke(tenantID, requestID)`
  once per active tenant the principal holds a membership in.
  A deferred `dumpMetrics` helper logs one structured JSON info
  line per metric family in the registry, allowing Grafana Agent or
  Prometheus log-based service discovery to collect the sample. This
  is the log-dump emission pattern documented in ADR-065: the
  one-shot admin CLI does not gain a `/metrics` HTTP endpoint.

- `control-plane/auth/cmd/admin/metrics_emission_test.go`: Phase 22
  slice 48.2 test contracts. `TestDumpMetricsLogsOneLinePerFamilyForRevokeSession`
  asserts that after one `ObserveSessionRevoke` call, the deferred
  `dumpMetrics` emits one structured log line with the correct
  `metric`, `tenant_count`, `tenants`, `operation`, and `component`
  fields. `TestDumpMetricsIsNoopForNonMetricOperations` asserts that
  a nil registry produces no output and no panic.

- `docs/observability/metrics-catalog.md`: Phase 22 slice 48.3
  (ADR-065) updates the `auth_session_revoke_total` row from
  "Recorder wired; integration pending because the admin CLI is one-shot"
  to the full log-dump emission description, including the
  per-tenant observation semantics, the Grafana Agent collection
  path, and the historical rate-query limitation for one-shot
  invocations.

- `docs/adr/adr-065-phase22-auth-session-revoke-emission.md`:
  Phase 22 umbrella decision. Records scope (slice 48.1 / 48.2 /
  48.3: `auth_session_revoke_total` emission via admin CLI), the
  log-dump emission pattern for one-shot CLI, the serializable
  transaction design for `RevokeSessionsForPrincipal`, non-goals
  (slices 43.1.5 / 43.3.5 / Java 26.F9), and the outcome-contract
  change note for empty outcome labels (Phase 21 / ADR-063 / v0.6.0).

<!-- Add new Phase content above this line. -->


## [v0.6.0] - 2026-09-08

## [v0.5.0] - 2026-09-08

## [v0.4.0] - 2026-09-08

## [v0.3.0] - 2026-09-07

## [v0.2.0] - 2026-09-07

## [v0.1.0-phase0] - 2026-08-02

