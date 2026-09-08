# Phase 32 — Tenant-Id Label Translation at the SyncJob CR Writer

## Status

Active (ADR-081 Accepted). The Layer-1 test ships with one happy-
path case and one table-driven rejection case covering five
non-canonical UUID forms; all six cases green at
`go test ./console/internal/syncjobcr/... -count=1`. The phase
also adds one minimal defence-in-depth guard to
`realDualWriter.create` so the rejection cases can short-circuit
locally (without contacting the API server) and emit
`OutcomeInvalid`.

## Goal

Phase 31 closed the **server-side** half of the tenant-id
envelope chain (BFF egress → API Server interceptor → mutation
repository → `astrasync_control_jobs.tenant_id`). Phase 32
closes the **client-side half of the chain** at a boundary that
no existing test pinned: the JSON body that
`console/internal/syncjobcr.realDualWriter` sends to the
Kubernetes API server. The contract is:

- `realDualWriter.create` MUST write `Scope.TenantID` verbatim
  into `metadata.labels["astrasync.io/tenant-id"]` (no
  whitespace change, no case change, no brace change, no prefix
  change).
- `realDualWriter.create` MUST refuse non-canonical `Scope.TenantID`
  locally (defence in depth) and emit `OutcomeInvalid` without
  contacting the API server. The K8s API server's CEL rule
  (`XValidation` on `astrasync.io/tenant-id`) is the
  authoritative secondary check; the writer is the primary check.

The controller-side pin (`controller_job_state_total{tenant_id}`
emitted from the label) continues to be covered by
`control-plane/controller/internal/controller/syncjob_emission_test.go`.

## What ships in Phase 32

A single-package Layer-1 test file under
`console/internal/syncjobcr/manager_label_translation_test.go`
plus one minimal production-code guard in
`console/internal/syncjobcr/manager.go` (the guard is required
for the rejection cases to short-circuit locally; the rest of
the writer is unchanged).

```
console/internal/syncjobcr/
├── manager.go
└── manager_label_translation_test.go
```

The test exercises `realDualWriter` (the production writer) via
a controlled `httptest.NewTLSServer` that captures the JSON body
the writer emits. The test follows the project's existing
conventions (testing.mdc):

- in-package (`package syncjobcr`),
- table-driven `t.Run("case_name", ...)` with snake_case names,
- `httptest.NewTLSServer` + `defer srv.Close()` for resource
  release,
- no `time.Sleep`,
- no `t.Skip`.

## Test coverage

Six cases, all under
`console/internal/syncjobcr/manager_label_translation_test.go`:

| Test | Asserts |
| --- | --- |
| `TestLabelTranslationPreservesCanonicalUUID` | `Scope.TenantID` is written verbatim into `metadata.labels["astrasync.io/tenant-id"]`; no whitespace, no case change, no brace change. |
| `TestLabelTranslationRejectsNonCanonicalTenantIDs/uppercase_uuid` | `Scope.TenantID = "AAAAAAAA-..."` → writer returns `OutcomeInvalid`, server is not invoked. |
| `TestLabelTranslationRejectsNonCanonicalTenantIDs/braced_uuid` | `Scope.TenantID = "{11111111-...}"` → `OutcomeInvalid`. |
| `TestLabelTranslationRejectsNonCanonicalTenantIDs/urn_prefixed_uuid` | `Scope.TenantID = "urn:uuid:..."` → `OutcomeInvalid`. |
| `TestLabelTranslationRejectsNonCanonicalTenantIDs/whitespace_padded_uuid` | `Scope.TenantID = " ..."` → `OutcomeInvalid`. |
| `TestLabelTranslationRejectsNonCanonicalTenantIDs/empty_tenant_id` | `Scope.TenantID = ""` → `OutcomeInvalid`. |

The five rejection cases are a single table-driven test
(`TestLabelTranslationRejectsNonCanonicalTenantIDs`); each
subtest uses a fresh `httptest.NewTLSServer` so failures
attribute to a single case. The
`TestLabelTranslationRejectsNonCanonicalUUIDs_DocumentedWhy`
documentation anchor is included to make the regression class
("the writer accidentally normalises the tenant-id before
serialising it") greppable from the test source.

## Production code change

`console/internal/syncjobcr/manager.go` gains one guard at the
top of `realDualWriter.create`:

```go
if !IsCanonicalTenantID(input.Scope.TenantID) {
    return OutcomeInvalid
}
```

`IsCanonicalTenantID` is the local canonical-UUID matcher that
mirrors `control-plane/auth.tenantIDPattern` (ADR-071 §2 +
ADR-081 §Context). The guard is **defence in depth**: the
BFF ingress already rejects non-canonical UUIDs (Phase 28-A
ADR-072, Phase 29 ADR-074), so this path should never be hit
in production. The guard ensures that if a future refactor
breaks the BFF validation, the Console still emits
`controller_syncjob_console_dual_write_total{outcome="invalid"}`
rather than the misleading `outcome="success"` it would emit
if the request were sent and admitted without a CEL check.

## Why a single-package test, not a cross-module extension

A cross-module test that wires Console → CR writer → controller
emission would require either:

1. **`envtest`** (kubebuilder's apiserver + etcd launcher) — a
   non-trivial transitive dependency
   (`k8s.io/apimachinery`, `k8s.io/client-go`,
   `sigs.k8s.io/controller-runtime`, plus etcd binaries).
   These dependencies are deliberately **not** imported by
   `console/` (see `manager.go`'s package doc).
2. **`fake.NewSimpleClientset`** — sufficient for
   `controller-runtime`'s reconcile path but does NOT exercise
   the actual CEL validator. A passing test against the fake
   would still let `Scope.TenantID = "AAAAAAAA-..."` through.

Phase 32 accepts the third option: exercise the JSON body the
writer produces against an `httptest.NewTLSServer` that parses
the body and validates the label shape. This pins the
**production** contract (the exact JSON the writer emits)
without depending on K8s machinery.

A future Phase 33 may revisit envtest under ADR-082 if the
project wishes to drive the full controller reconcile loop
end-to-end. That decision is out of scope here.

## CI integration

The Phase 32 test runs under the existing `console` Go module
CI lane; no new workflow file. The test is picked up by
`go test ./console/internal/syncjobcr/...`.

## Non-Goals (this phase)

- **No update-mutation coverage.** `update` and `delete` use the
  same label-translation logic as `create`
  (`manager.go` line 393 — Update sets
  `existing.Metadata.Labels = map[string]string{TenantLabelKey: input.Scope.TenantID}`).
  A duplicate `update` test is a Phase 33 follow-up
  (ADR-081 §Follow-ups).
- **No controller module import into `console/`.** The
  controller-side pin
  (`controller_job_state_total{tenant_id}` emission) continues
  to live in
  `control-plane/controller/internal/controller/syncjob_emission_test.go`.
- **No envtest.** envtest depends on etcd binaries, which
  inflate the test binary and the CI lane. ADR-081 §Decision
  defers envtest to a separate ADR-082 follow-up.
- **No new top-level dependency in `console/go.mod`.**
- **No new helm chart, no new metric, no new RBAC, no new audit
  event.**

## Acceptance criteria

- [x] `console/internal/syncjobcr/manager_label_translation_test.go`
      ships with one happy-path case and five table-driven
      rejection cases.
- [x] All six cases pass on
      `go test ./console/internal/syncjobcr/... -count=1`.
- [x] `realDualWriter.create` short-circuits non-canonical
      `Scope.TenantID` and emits `OutcomeInvalid` without
      contacting the API server.
- [x] `go vet ./console/...` is clean.
- [x] All other `console/` tests remain green
      (`go test ./console/... -count=1`).
- [x] `CHANGELOG.md` Unreleased / Added entry summarises the
      contract.
- [x] `docs/adr/adr-081-phase32-label-translation-cr-writer.md`
      Accepted.
- [x] `docs/adr/README.md` index updated to add the ADR-081 row.

## Notes on scope

Phase 32 ships the **minimum production code change** required
to make the rejection cases short-circuit locally: one guard at
the top of `realDualWriter.create`. The corresponding
`update`-path guard is **not** in Phase 32 scope — it is a
Phase 33 follow-up, deferred per ADR-081 §Follow-ups. The
update path continues to rely on (a) the BFF ingress canonical-
UUID check (Phase 28-A / Phase 29) and (b) the K8s API server's
CEL `XValidation` rule (ADR-071 §2) as the authoritative
secondary check, exactly as it did before Phase 32.

## Follow-ups (out of scope for Phase 32)

- **Phase 33 (envtest)**: extend the cross-module fixture to
  drive the controller reconcile loop with the SyncJob CRD
  registered against `envtest`'s apiserver. ADR-082, when
  written, must weigh envtest's binary overhead against the
  coverage.
- **Update-mutation guard**: add the same
  `IsCanonicalTenantID` guard to `realDualWriter.update`. The
  production code uses the same translation logic for `update`
  as for `create`; this is a defence-in-depth follow-up, not a
  new contract.
- **Update-mutation test**: add a focused single-package test
  that pins `update` carries the same label translation
  (`manager.go` line 393). Today's test covers `create`.

## Rollback

The phase is test-only plus a 4-line guard in
`realDualWriter.create`. Removing
`console/internal/syncjobcr/manager_label_translation_test.go`
and the 4-line guard reverts the slice. The cross-module
fixture, the controller emission test, and the BFF ingress
canonical-UUID check all remain unchanged, so the existing
defence-in-depth chain is preserved at the second line of
defence (BFF ingress) and the third (K8s CEL `XValidation`).