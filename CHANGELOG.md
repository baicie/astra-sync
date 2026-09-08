# Changelog

All notable changes to this project are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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

