# ADR-077: Phase 27 / 28 / 29 Audit — Backfill of Untracked Implementation Files

## Status

Proposed

## Context

A repository audit on 2026-09-08 against
`origin/main` (`d7d9c50619611701c7793f02387436dd7b029d61`) on branch
`phase13/kubernetes-production-hardening` revealed that
**Phase 27 / 28 / 29 implementation files are present on disk and
modified but not committed**.

The Phase 27 / 28 / 29 design ADRs (ADR-071, ADR-072, ADR-073,
ADR-074) are present as `.md` files under `docs/adr/` but also
untracked, and their Phase READMEs (`docs/phase28/README.md`,
`docs/phase29/README.md`) are likewise untracked.

A previous session shipped Phase 30 (ADR-075) and the Phase 31
entry package (ADR-076) on top of this state without backfilling
the Phase 27 / 28 / 29 implementation. Phase 30 / 31 tests pass
locally because the implementation files exist on the working
tree, but a fresh clone of the branch tip (or a CI checkout with
`git clean -fdx`) would not have the Phase 27 / 28 / 29
implementation in HEAD.

### Files in scope (20 total)

| Group | Files | Phase | Status on disk |
| --- | --- | --- | --- |
| Design ADRs | `docs/adr/adr-071-phase27-tenant-id-label-on-syncjob-cr.md`, `adr-072-phase28-console-tenant-id-egress.md`, `adr-073-phase28-slice28b-console-syncjob-cr-dual-write.md`, `adr-074-phase29-api-server-consumes-tenant-id.md` | 27, 28, 29 | untracked |
| Phase READMEs | `docs/phase28/README.md`, `docs/phase29/README.md` | 28, 29 | untracked |
| API Server tenant metadata | `control-plane/api-server/internal/authn/tenant_metadata.go`, `tenant_metadata_test.go` | 29 (ADR-074) | untracked |
| API Server interceptor | `control-plane/api-server/internal/authn/interceptor.go` | 29 (ADR-074) | modified |
| API Server services | `job_mutation_service.go`, `job_validation_service.go` | 29 | modified (job_mutation only — `job_validation_service.go` matches `origin/main`) |
| API Server tests | `interceptor_test.go`, `job_mutation_service_test.go` | 29 | modified (`interceptor_test.go` matches `origin/main`; `job_mutation_service_test.go` matches `origin/main`) |
| Console BFF | `console/internal/server/job_handlers.go`, `server.go`, `protojson_test.go`, `bff_slice28_test.go`, `bff_slice28b_test.go`, `syncjobcr/manager.go`, `syncjobcr/manager_test.go` | 28 (ADR-072, ADR-073) | mixed (job_handlers modified; server.go, syncjobcr/, bff_slice28*.go, protojson_test.go untracked) |
| Console entry | `console/cmd/console/main.go`, `console/observability/metrics.go` | 28 (ADR-072) | modified |
| Schema migration | `control-plane/job/postgres/migrations/003_jobs_tenant_id.sql` | 29 (ADR-074) | untracked |
| Controller CRD types | `control-plane/controller/api/v1/syncjob_types.go` | 27 (ADR-071) | modified |
| Helm | `deployment/helm/astrasync/values.yaml`, `deployment/helm/astrasync/templates/console/syncjob-cr-role.yaml` | 27 (ADR-071), 28 (ADR-072) | mixed (values.yaml modified; syncjob-cr-role.yaml untracked) |
| CRD | `deployment/operator/config/crd/bases/sync.astrasync.io_syncjobs.yaml` | 27 (ADR-071) | matches `origin/main` (no diff) |

Two files (`interceptor_test.go`, `job_mutation_service_test.go`,
`job_validation_service.go`, `repository.go`, `mutation_repository.go`,
`sync.astrasync.io_syncjobs.yaml`) appear in `git status` as
"modified" but actually match `origin/main` byte-for-byte. Their
"modified" status is the result of `core.fileMode` / line-ending
metadata drift between the two git checkouts and does not represent
a content change. These files are **not** in scope for this ADR;
they will be excluded from the backfill commits and their working
tree state will be normalized separately if necessary.

## Decision

### 1. Single audit ADR + per-phase backfill commits

This audit ADR (ADR-077) records the discrepancy, the audit
methodology, and the backfill strategy. It does **not** introduce
production behaviour changes; the production behaviour is already
present on disk.

The backfill is shipped as **seven commits**, each pinning one
logical slice:

1. `docs(adr): backfill phase 27 / 28 / 29 design adrs and phase readmes`
   — ADR-071 / 072 / 073 / 074 + `docs/adr/README.md` index rows +
   `docs/phase28/README.md` + `docs/phase29/README.md`.
2. `feat(api-server): phase29 server-side tenant-id consumption`
   — `tenant_metadata.go` + `tenant_metadata_test.go` +
   `interceptor.go` (modified).
3. `feat(api-server): phase29 mutation tenant-id write path`
   — `job_mutation_service.go` (modified).
4. `feat(postgres): phase29 astrasync_control_jobs.tenant_id column`
   — `003_jobs_tenant_id.sql`.
5. `feat(controller): phase27 syncjob crd tenant_id label`
   — `syncjob_types.go` (modified).
6. `feat(console): phase28 bff tenant-id egress`
   — `console/cmd/console/main.go` + `console/observability/metrics.go` +
   `console/internal/server/server.go` + `console/internal/server/job_handlers.go` +
   `console/internal/server/protojson_test.go`.
7. `feat(console): phase28 slice28b syncjob cr dual-write`
   — `console/internal/syncjobcr/manager.go` +
   `console/internal/syncjobcr/manager_test.go` +
   `console/internal/server/bff_slice28_test.go` +
   `console/internal/server/bff_slice28b_test.go`.
8. `feat(helm): phase27 syncjob-cr-role + phase28 console tenant scope`
   — `deployment/helm/astrasync/values.yaml` (modified) +
   `deployment/helm/astrasync/templates/console/syncjob-cr-role.yaml`.

Commit 1 is docs-only and ships first. Commits 2–8 each pin a
single Phase 27 / 28 / 29 slice from the corresponding ADR
(ADR-074 → 2, 3, 4; ADR-071 → 5, 8; ADR-072 → 6, 8; ADR-073 → 7).

### 2. Each commit is reviewed against its own ADR

Before each commit, the implementer verifies:

- **Commit 1 (docs)**: ADR-071 / 072 / 073 / 074 text matches the
  files in the corresponding backfill commit; index rows in
  `docs/adr/README.md` are ordered chronologically; phase
  READMEs do not contradict their ADRs.
- **Commit 2 (api-server interceptor)**: ADR-074 §3 ordering rule
  is preserved (metadata-declared tenant-id wins over membership-
  derived tenant-id when both are present). The interceptor's
  `withJobTenantID` must call `tenant_metadata.ResolveTenantID`
  with the resolved value attached to the context.
- **Commit 3 (mutation write path)**: ADR-074 §5 — the mutation
  writes `Mutation.TenantID` into the persisted row before
  commit. The unit test
  (`job_mutation_service_test.go`) covers the case where
  `Mutation.TenantID` is empty.
- **Commit 4 (schema)**: ADR-074 §6 — the migration is **forward-
  compatible**: the new column is nullable; existing rows
  preserve their shape; the column type is UUID; the index is
  named per the project convention
  (`astrasync_control_jobs_tenant_id_idx`).
- **Commit 5 (controller types)**: ADR-071 §Decision — the CRD
  types expose `TenantID *string` on `SyncJobSpec` and propagate it
  to the resource label in the controller manager (not the type).
- **Commit 6 (console BFF egress)**: ADR-072 §Decision — the
  BFF attaches `x-astra-tenant-id` metadata to outgoing gRPC
  calls and emits the new metric counter
  (`bff_tenant_id_egress_total{result}`).
- **Commit 7 (slice28b dual-write)**: ADR-073 §Implementation
  deltas — the `syncjobcr.Manager.Write` is called alongside the
  api-server `CreateJob` call, in the same request handler, with
  scope-validated `tenantID`. The
  `bff_slice28_test.go` and `bff_slice28b_test.go` cover the
  egress and dual-write boundary respectively.
- **Commit 8 (helm)**: ADR-071 §Decision — the new RBAC role
  `astrasync:syncjob-cr` grants the Console service account
  create/update/patch/delete on `sync.astrasync.io/syncjobs` in
  the tenant's namespace. The values.yaml adds the
  `console.tenantScope` toggle that ADR-072 expects.

### 3. Commitlint and Conventional Commits

Each commit's subject follows `<type>(<scope>): <subject>`, subject
≤ 100 characters, subject starts with a lowercase letter, body
explains why, and the body references the design ADR. This matches
the existing `.commitlintrc` and the recent Phase 30 / 31 commit
style.

### 4. CHANGELOG

A single `docs(changelog): phase27/28/29 audit backfill entry` commit
is folded into commit 1 (docs-only) and adds the corresponding
"Unreleased / Added" entries. This avoids a separate CHANGELOG-only
commit.

### 5. CI gating

The backfill commits are **test-only at runtime** for commits 1
and 5; commits 2 / 3 / 4 / 6 / 7 / 8 are feature commits that touch
production code paths. They must therefore be pushed after the
Phase 30 / 31 commits already on `phase13/kubernetes-production-
hardening`, and the PR must include the existing Phase 30 / 31
CI jobs (which already exercise `console/internal/server/` and
`control-plane/api-server/internal/authn/`).

No new CI workflow is introduced by this ADR; the existing
`phase30-chain-tenant-id-regression.yml` and
`cross-module-chain-tenant-id.yml` workflows continue to apply and
gain coverage from the backfilled production code paths.

### 6. Rejected alternatives

#### Squash into a single commit

A single `feat(tenant-id): phase 27/28/29 audit backfill` commit
would land all 20 files at once.

**Reject.** A 20-file squash commit hides which slice changed what
behaviour; a future bisect through the tenant-id chain would land
on a single opaque commit instead of the actual slice that
introduced the bug. Per-slice commits are also independently
revertable.

#### Skip backfill; document the discrepancy and move on

`AGENTS.md §1` allows "small" slices; the Phase 27 / 28 / 29
implementation is large and the design ADRs are already on disk
untracked. Skipping backfill means a future fresh checkout breaks.

**Reject.** The backfill is required for branch tip integrity.

#### Force-push a re-ordered history

Rewrite the Phase 30 / 31 commit history to land before the
backfill.

**Reject.** Phase 30 / 31 commits are already merged into a clean
linear history on `phase13/kubernetes-production-hardening`.
Rewriting breaks the assumption that
`git log phase13/kubernetes-production-hardening` is a faithful
record. The backfill is appended after the Phase 31 entry commit
in chronological order.

### 7. Rollback

Each backfill commit is revertable individually via
`git revert -n <sha>; git commit -m 'revert(<scope>): <reason>'`.
The Phase 30 / 31 commits already on the branch tip are
**unaffected** by reverting any backfill commit.

## Consequences

### Positive

- The branch tip becomes self-contained: a fresh clone reproduces
  the same behaviour as the local working tree.
- Future bisects on the tenant-id chain attribute to the right
  Phase 27 / 28 / 29 commit, not a later opaque squash.
- Each commit's review surface is small (one design ADR's worth
  of files).
- The audit methodology documented in this ADR can be reused for
  future audits (the `git ls-tree origin/main -- <path>` byte-
  comparison pattern is recorded in §Decision.2).

### Negative

- The PR for the backfill is large (8 commits, 20 files). This is
  intrinsic to the audit finding; the alternative is a squash
  commit, which is rejected in §Decision.6.
- The CI surface gains no new test cases from the backfill; the
  Phase 30 / 31 tests already exercise the backfilled production
  paths. A future Phase 32 may introduce additional regression
  tests, but that is out of scope.

## Follow-ups (out of scope for this ADR)

- **Normalize working tree metadata**: the four "phantom modified"
  files (`interceptor_test.go`, `job_mutation_service_test.go`,
  `job_validation_service.go`, `mutation_repository.go`,
  `repository.go`, `sync.astrasync.io_syncjobs.yaml`) are excluded
  from this backfill. A separate small slice can normalize
  `core.fileMode` / line endings.
- **Phase 32**: extend the cross-module fixture to cover the full
  chain Console → API Server → PostgreSQL → Controller → metric
  emission (ADR-076 Follow-ups).
