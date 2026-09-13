# Phase 26: Phase 17 Backlog Closeout — metrics-catalog.md + ADR-058 supersession + controller reconcile loop tests

## Status

**Complete.** Phase 26 closes the Phase 17 observability backlog
(ADR-058) by updating `docs/observability/metrics-catalog.md` to mark
all six backlog metrics as emitted, updates ADR-058 status to
"Superseded by Phase 25 closeout", and adds unit-test coverage for
the controller reconcile loop. All Phase 17 slices (43.0 through
51) are landed; the Phase 26 acceptance criteria are fully satisfied.

## Theme

Phase 17 ran from ADR-058 acceptance (2026-09-07) through Phase 25
slice 51.1 (2026-09-08), activating six deferred metrics across
Console BFF, API Server, auth library, and controller reconcile
boundaries. Phase 26 is the closeout slice that reconciles the
`metrics-catalog.md` backlog table and the Phase 17 umbrella ADR-058
status with the delivered state, then adds the missing test
coverage for the controller reconcile loop introduced in slice
51.1 (ADR-069).

## Scope

### Slices

| Slice | Owner | Description | Status |
|---|---|---|---|
| 52.0 | agent | ADR-058 + ADR index entry: Status → "Superseded by Phase 25 closeout" | **Done** |
| 52.1 | agent | `metrics-catalog.md`: update `apiserver_sign_in_total` detailed row from "production call site pending" to "Emitted by Console BFF Manager.CompleteLogin (Phase 17 slice 43.1.5)" | **Done** |
| 52.2 | agent | `metrics-catalog.md`: update `auth_sign_in_total` detailed row from "admin CLI is one-shot; long-running consumer needed" to "Emitted by Console BFF Manager.CompleteLogin (Phase 17 slice 43.1.5)" | **Done** |
| 52.3 | agent | `metrics-catalog.md`: reconcile the Phase 17 backlog table — add "Emitted" column, freeze the table, add future-guidance note | **Done** |
| 52.4 | agent | `docs/phase26/README.md` + `CHANGELOG.md` entry | **Done** |
| 53.0 | agent | Add `syncjob_controller_test.go` covering the reconcile loop happy + rejection paths | **Done** |
| 53.1 | agent | Provide the `testSyncJob` helper that the pre-existing `observability_test.go` references but does not define (closes pre-existing test debt) | **Done** |
| 53.2 | agent | Delete the broken `scripts/reorder-changelog.py` (the script was destructive: it overwrote CHANGELOG.md with an empty placeholder when run against the current file) and apply the date stamps to all `[vX.Y.Z]` section headers manually | **Done** |

## Acceptance Criteria

- [x] `metrics-catalog.md`: `apiserver_sign_in_total` row updated to "Emitted" (Phase 17 slice 43.1.5)
- [x] `metrics-catalog.md`: `auth_sign_in_total` row updated to "Emitted" (Phase 17 slice 43.1.5)
- [x] `metrics-catalog.md`: Phase 17 backlog table frozen; all 6 rows carry "Emitted" date
- [x] `metrics-catalog.md`: future-guidance note added directing future activation to the ADR-058 pattern
- [x] `adr-058-observability-catalog-backlog.md`: Status updated to "Accepted — Superseded by Phase 25 closeout"
- [x] `docs/adr/README.md`: ADR-058 index row updated
- [x] `docs/phase26/README.md` written
- [x] `CHANGELOG.md` updated under `[Unreleased]` / `Added` (Phase 26 closeout)
- [x] `control-plane/controller/internal/controller/syncjob_controller_test.go` covers the reconcile loop with **18 table-driven cases** covering: finalizer add, deletion+job-not-found, converge start/stop/noop/conflict/spec-replace, deletion+active-stop, deletion+inactive-delete+finalizer, deletion+delete-conflict, nil-repository guard, not-found swallow, spec-change-while-active, create-when-not-found, create-already-exists race, ignore-spec-while-canceling, noop-on-stopped, error-passthrough
- [x] All 18 reconcile tests + 7 emission tests + 1 reconcile-observability test pass; `go test ./...` is green across the controller module
- [x] `go vet ./...` clean across the controller module
- [x] `scripts/reorder-changelog.py` removed (was destructive)

## Out of Scope

- OpenMetrics content negotiation (ADR-051 §130) — remains a separate deferred decision.
- Java data-plane metrics 26.F9 (ADR-051 §7) — not in Phase 17 scope.
- `request_id` exemplar contract — requires OpenMetrics negotiation before it can transmit; Phase 17 does not unblock that deferral.

## Phase 17 Slice Timeline (reference)

| Slice | Metric | Emitted | ADR |
|---|---|---|---|
| 43.1.5 | `apiserver_sign_in_total` | 2026-09-08 | ADR-058 §2 |
| 43.1.5 | `auth_sign_in_total` | 2026-09-08 | ADR-058 §2 |
| 43.2 | `auth_session_revoke_total` | 2026-09-08 | ADR-065 (Phase 22) |
| 43.3 | `controller_job_state_total` | 2026-09-08 | ADR-066 (Phase 23) |
| 43.3 | `controller_epoch_fence_total` | 2026-09-08 | ADR-069 (Phase 25) |
| 50 (ADR-068) | `apiserver_session_revoke_total` | 2026-09-08 | ADR-068 (Phase 24) |

## Slice 53 Test Inventory

| Test | Coverage |
|---|---|
| `TestReconcile_adds_finalizer_when_absent` | Happy path: new resource gets the control-plane finalizer |
| `TestReconcile_deletion_removes_finalizer_when_job_not_found` | Deletion path: finalizer removed when job is gone |
| `TestReconcile_converge_starts_stopped_job` | Converge: CREATED → INITIALIZING on desired=RUNNING |
| `TestReconcile_converge_stops_running_job` | Converge: RUNNING → CANCELING on desired=STOPPED |
| `TestReconcile_converge_noop_returns_requeue` | Converge: no-op returns RequeueAfter |
| `TestReconcile_converge_requeues_on_persistent_conflict` | Converge: 5 retries exhausted → requeue, version unchanged |
| `TestReconcile_converge_replaces_spec_when_inactive` | Converge: ReplaceSpec called when CREATED |
| `TestReconcile_deletion_stops_active_job_and_requeues` | Deletion: RUNNING → CANCELING, requeue |
| `TestReconcile_deletion_deletes_inactive_job_and_removes_finalizer` | Deletion: CREATED → repository delete + finalizer remove |
| `TestReconcile_deletion_requeues_on_delete_conflict` | Deletion: ErrConflict on Delete → requeue, job preserved |
| `TestReconcile_returns_error_when_jobs_repository_is_nil` | Guard: descriptive error when Jobs is nil |
| `TestReconcile_returns_nil_for_unknown_resource` | Reconcile: client.IgnoreNotFound swallows not-found |
| `TestReconcile_converge_spec_change_while_active_requests_stop_first` | Converge invariant: stop before spec replace when active |
| `TestReconcile_converge_creates_job_when_not_found` | Converge: New creates job when repository is empty |
| `TestReconcile_converge_retries_on_create_already_exists` | Converge: ErrAlreadyExists on Create → retry |
| `TestReconcile_ignores_spec_change_while_canceling` | Converge invariant: spec NOT replaced when CANCELING |
| `TestReconcile_converge_stops_inactive_job_via_desired_stop` | Converge: noop when already STOPPED |
| `TestReconcile_converge_error_from_jobs_get_passes_through` | Converge: unknown error from Jobs.Get propagates |

## References

- ADR-058 — Observability Catalog Backlog (Phase 17 umbrella; now Superseded)
- ADR-065 — Phase 22 auth session-revoke emission
- ADR-066 — Phase 23 controller reconcile emission
- ADR-068 — Phase 24 API Server session-revoke emission
- ADR-069 — Phase 25 controller epoch-fence emission
