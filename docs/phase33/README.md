# Phase 33 — Update-Mutation Tenant-Id Guard at the SyncJob CR Writer

## Status

Active (ADR-082 Accepted). The Layer-1 test ships with three
new cases (one happy-path, one table-driven rejection, one
guard-ordering) covering the update mutation symmetrically with
the Phase 32 create-path coverage. All twelve
label-translation cases in
`console/internal/syncjobcr/manager_label_translation_test.go`
are green at `go test ./console/internal/syncjobcr/... -count=1`.

## Goal

Phase 32 (ADR-081) closed the **create-path** half of the
tenant-id label-translation contract at the SyncJob CR writer
boundary. Phase 33 closes the **update-path** half: the writer
must short-circuit non-canonical `Scope.TenantID` locally on
the update mutation, before the discovery GET, with the same
diagnostic metric split (`invalid` vs `admission_rejected`) as
the create path.

## What ships in Phase 33

A single-package Layer-1 test addition under
`console/internal/syncjobcr/manager_label_translation_test.go`
plus a 4-line production-code guard at the top of
`realDualWriter.update` in
`console/internal/syncjobcr/manager.go`.

```
console/internal/syncjobcr/
├── manager.go                                # +4 lines (update guard)
└── manager_label_translation_test.go         # +3 cases
```

## Test coverage

Three new cases in
`console/internal/syncjobcr/manager_label_translation_test.go`:

| Test | Asserts |
| --- | --- |
| `TestLabelTranslationUpdatePreservesCanonicalUUID` | On a successful update, the PUT body's `metadata.labels["astrasync.io/tenant-id"]` is `Scope.TenantID` verbatim; the stale label value from the existing CR is replaced. |
| `TestLabelTranslationUpdateRejectsNonCanonicalTenantIDs/{uppercase_uuid,braced_uuid,urn_prefixed_uuid,whitespace_padded_uuid,empty_tenant_id}` | Writer returns `OutcomeInvalid`; the fake apiserver is not invoked (neither GET nor PUT). |
| `TestLabelTranslationUpdateGuardShortCircuitsBeforeGET` | Writer fires the guard BEFORE the discovery GET — `get_hits == 0`, `put_hits == 0`. |

The five rejection cases reuse the table-driven pattern from
`TestLabelTranslationRejectsNonCanonicalTenantIDs` so the
coverage matrix is symmetric across the create / update
mutations. The `short_circuits_before_get` case is unique to
the update path: it pins the order of the guard (before GET)
so a future refactor that moves the guard after the GET is
caught at unit-test speed.

## Production code change

`console/internal/syncjobcr/manager.go::realDualWriter.update`
gains one guard at the top of the function (mirroring
`realDualWriter.create` from ADR-081):

```go
if !IsCanonicalTenantID(input.Scope.TenantID) {
    return OutcomeInvalid
}
```

The guard fires **before** the discovery GET, so the writer
never touches the K8s API server when the input is malformed.
Firing after the GET would (a) waste the round-trip, (b) emit
an audit-log entry for a CR the writer never had business
touching, (c) mis-attribute the metric (a 200 OK on GET followed
by a CEL rejection on PUT would surface as
`OutcomeAdmissionRejected` rather than `OutcomeInvalid`),
breaking the Phase 32 metric diagnostic split.

## Why a guard, not just a test

The Phase 32 production-code guard was a side effect of making
the Layer-1 tests short-circuit locally (the alternative was
to let the request reach the K8s API server and assert on the
422 admission rejection — but that contract mis-attributes the
metric). The same logic applies to the update path: a test
that asserts on `OutcomeAdmissionRejected` would work, but the
Phase 32 regression in `TestRealDualWriterCreateAdmissionRejected`
shows that path is brittle. The guard is the only way to
preserve the diagnostic split on both paths.

## CI integration

The Phase 33 test runs under the existing `console` Go module
CI lane; no new workflow file. The test is picked up by
`go test ./console/internal/syncjobcr/...`.

## Non-Goals (this phase)

- **No envtest.** envtest depends on etcd binaries, which
  inflate the test binary and the CI lane. ADR-081 §Decision
  defers envtest to a separate ADR (now ADR-082 §Follow-ups);
  ADR-082 itself only covers the update-path guard.
- **No new top-level dependency in `console/go.mod`.**
- **No new helm chart, no new metric, no new RBAC, no new audit
  event.**
- **No `delete`-path coverage.** The `delete` mutation does
  not write `metadata.labels` (it issues a DELETE on the
  resource URL); there is no label-translation contract to
  pin on the `delete` path. The existing
  `TestRealDualWriterDelete*` tests cover the deletion
  semantics.

## Acceptance criteria

- [x] `console/internal/syncjobcr/manager_label_translation_test.go`
      gains three new cases (1 happy-path + 1 table-driven
      rejection + 1 guard-ordering).
- [x] All twelve label-translation cases pass on
      `go test ./console/internal/syncjobcr/... -count=1`.
- [x] `realDualWriter.update` short-circuits non-canonical
      `Scope.TenantID` BEFORE the discovery GET and emits
      `OutcomeInvalid` without contacting the API server.
- [x] `go vet ./console/...` is clean.
- [x] All other `console/` tests remain green
      (`go test ./console/... -count=1`).
- [x] `CHANGELOG.md` Unreleased / Added entry summarises the
      contract.
- [x] `docs/adr/adr-082-phase33-update-mutation-tenant-id-guard.md`
      Accepted.
- [x] `docs/adr/README.md` index updated to add the ADR-082 row.

## Notes on scope

Phase 33 ships the **minimum production code change** required
to make the update-path rejection cases short-circuit locally:
one 4-line guard at the top of `realDualWriter.update`. The
guard mirrors the `create`-path guard (ADR-081 §Decision)
verbatim; no helper extraction, no shared symbol. Helper
extraction is a future refactor ADR if a third call site needs
the same guard (ADR-082 §Follow-ups).

The `delete` mutation does not write `metadata.labels` and is
therefore not in Phase 33 scope.

## Follow-ups (out of scope for Phase 33)

- **Phase 34 (envtest)**: extend the cross-module fixture to
  drive the controller reconcile loop with the SyncJob CRD
  registered against `envtest`'s apiserver. ADR-082 §Follow-ups.
- **Helper extraction**: if a third call site needs the same
  `IsCanonicalTenantID` guard, extract a
  `validateCanonicalTenantID(input)` helper. ADR-082
  §Alternatives Considered.

## Rollback

The phase is test-only plus a 4-line guard in
`realDualWriter.update`. Removing the three new test cases and
the guard reverts the slice. The BFF ingress canonical-UUID
check (Phase 28-A / Phase 29) and the K8s API server's CEL
`XValidation` rule on `astrasync.io/tenant-id` (ADR-071 §2)
remain the upstream and downstream lines of defence for the
update path, exactly as they did before Phase 33.