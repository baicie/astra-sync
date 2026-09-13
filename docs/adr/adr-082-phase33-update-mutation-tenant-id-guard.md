# ADR-082: Phase 33 — Update-Mutation Tenant-Id Guard at the SyncJob CR Writer

## Status

Accepted

> **Correction:** ADR-086 supersedes ADR-071 §2. The CRD CEL rule
> described below was not installable; ADR-087 restores admission
> enforcement with a ValidatingAdmissionPolicy, and the Phase 33 writer
> guard remains the local boundary.

## Context

Phase 32 (ADR-081) closed the **create-path** half of the
tenant-id label-translation contract at the SyncJob CR writer
boundary:

```
BFF egress → realDualWriter.create → metadata.labels[astrasync.io/tenant-id]
```

The `create` path now refuses non-canonical `Scope.TenantID`
locally and emits `OutcomeInvalid` (defence in depth) before
the JSON body reaches the K8s API server.

Phase 32 §Follow-ups (ADR-081 §Follow-ups) deferred **two**
follow-ups to Phase 33:

> - **Update-mutation guard**: add the same `IsCanonicalTenantID`
>   guard to `realDualWriter.update`. The production code uses
>   the same translation logic for `update` as for `create`; this
>   is a defence-in-depth follow-up, not a new contract.
> - **Update-mutation test**: add a focused single-package test
>   that pins `update` carries the same label translation
>   (`manager.go` line 393). Today's test covers `create`.

The `update` path applies the same translation logic as `create`
— `realDualWriter.update` reads the existing CR via a discovery
GET, overwrites `existing.Metadata.Labels` with
`map[string]string{TenantLabelKey: input.Scope.TenantID}`, and
PUTs the result. Without a `IsCanonicalTenantID` guard on the
update path, a non-canonical `Scope.TenantID` reaching the
writer:

1. Issues a discovery GET against the K8s API server (consumes
   network bandwidth, rate-limit budget, and audit-log volume
   for a CR the writer never had business touching).
2. Reads the existing `metadata.labels["astrasync.io/tenant-id"]`
   (whatever stale value is there).
3. Overwrites it with the non-canonical `Scope.TenantID`.
4. PUTs the result.
5. **Only then** is rejected by the K8s API server's CEL
   `XValidation` rule (ADR-071 §2), returning HTTP 422 →
   `OutcomeAdmissionRejected`.

The metric outcome is therefore `admission_rejected` rather than
`invalid`, which conflates "the apiserver rejected a CEL rule"
with "the writer actually attempted to write something". This
breaks the dual-write metric's diagnostic split (Phase 32
regression in `TestRealDualWriterCreateAdmissionRejected`,
ADR-081 §Negative).

## Decision

Phase 33 ships two artefacts:

1. **Production-code guard** — one `IsCanonicalTenantID` short-
   circuit at the top of `realDualWriter.update`, mirroring the
   `create`-path guard (ADR-081 §Decision). The guard fires
   **before** the discovery GET, so the writer never touches the
   K8s API server when the input is malformed.

2. **Layer-1 test** — three new cases under
   `console/internal/syncjobcr/manager_label_translation_test.go`,
   symmetric to the create-path tests:

| Test | Asserts |
| --- | --- |
| `TestLabelTranslationUpdatePreservesCanonicalUUID` | On a successful update, the PUT body's `metadata.labels["astrasync.io/tenant-id"]` is `Scope.TenantID` verbatim; the stale label value from the existing CR is replaced. |
| `TestLabelTranslationUpdateRejectsNonCanonicalTenantIDs/{uppercase,braced,urn_prefixed,whitespace_padded,empty}` | Writer returns `OutcomeInvalid`, server is not invoked (neither GET nor PUT). |
| `TestLabelTranslationUpdateGuardShortCircuitsBeforeGET` | Writer fires the guard BEFORE the discovery GET — `get_hits == 0`, `put_hits == 0`. |

The five rejection cases reuse the table-driven pattern from
`TestLabelTranslationRejectsNonCanonicalTenantIDs` so the
coverage matrix is symmetric across the create / update
mutations. The `short_circuits_before_get` case is unique to
the update path: it pins the order of the guard (before GET)
so a future refactor that moves the guard after the GET is
caught at unit-test speed.

### Why a guard, not just a test

The Phase 32 production-code guard was a side effect of making
the Layer-1 tests short-circuit locally (the alternative was to
let the request reach the K8s API server and assert on the 422
admission rejection — but that contract mis-attributes the
metric). The same logic applies to the update path: a test
that asserts on `OutcomeAdmissionRejected` would work, but the
Phase 32 finding (`TestRealDualWriterCreateAdmissionRejected`
regression) shows that path is brittle — it conflates
"writer refused" with "apiserver refused". The guard is the
only way to preserve the diagnostic split on both paths.

### Why the guard fires before the discovery GET

The update path's discovery GET is a **side-effecting read**
(network round-trip, audit-log entry, rate-limit budget). A
non-canonical `Scope.TenantID` is invalid input; firing the
guard after the GET would (a) waste the round-trip, (b) emit
an audit-log entry for a CR the writer never had business
touching, (c) make the metric attribution wrong (a 200 OK on
GET followed by a CEL rejection on PUT would surface as
`admission_rejected`, not `invalid`). The
`TestLabelTranslationUpdateGuardShortCircuitsBeforeGET` case
pins this ordering.

## Consequences

### Positive

- The `update` mutation is now symmetric with `create`: both
  paths short-circuit non-canonical tenant-ids locally and
  emit `controller_syncjob_console_dual_write_total{outcome="invalid"}`.
- The dual-write metric preserves the diagnostic split on both
  paths: `invalid` = writer refused; `admission_rejected` =
  apiserver refused at CEL validation; `success` = PUT
  succeeded.
- The `update` path no longer wastes a discovery GET round-trip
  on a malformed input.
- The Phase 32 happy-path contract (`metadata.labels["astrasync.io/tenant-id"]`
  verbatim) is now pinned for both `create` and `update`.

### Negative

- Phase 33 adds 3 new test cases (1 happy-path + 1 table-driven
  rejection + 1 short-circuit ordering); total Layer-1 coverage
  in `manager_label_translation_test.go` is now 12 cases (3
  happy-path + 2 table-driven rejections + 1 documentation
  anchor + 1 update happy-path + 1 update table-driven rejection
  + 1 update guard ordering + 1 update documented why).
- The `update`-path guard runs before the discovery GET; this
  adds a single regex match (cheap) per update call but is
  strictly necessary for the metric diagnostic split.

## Alternatives Considered

### Guard fires after the discovery GET

Place the `IsCanonicalTenantID` check after the existing CR is
loaded, before the PUT.

**Reject.** This would (a) leak the discovery GET round-trip
for a malformed input, (b) emit an audit-log entry for a CR the
writer never had business touching, (c) emit
`OutcomeAdmissionRejected` instead of `OutcomeInvalid` when the
PUT is rejected by CEL — breaking the Phase 32 metric
diagnostic split. Cost: 1 regex match per update call, paid for
in correctness.

### Reuse the create-path guard via a helper

Extract `validateCanonicalTenantID(input)` and call it from both
`create` and `update`.

**Accept in spirit, defer.** The two guards are 4 lines each and
identical; extracting a helper is a readability win but adds
one more symbol to the package surface. Phase 33 keeps the two
guards inline (so each path's contract is visible at the call
site) and leaves the helper extraction to a future refactor ADR
if the pattern grows beyond two call sites.

### Skip the test, rely on the guard

Drop the Layer-1 test; rely on the production-code guard.

**Reject.** The guard is a single-line regex match. A future
refactor that drops the guard or moves it after the GET would
not be caught by the existing `chain_e2e_test.go` (which only
feeds canonical UUIDs). The 3 new test cases pin the contract
at the same Layer-1 surface as Phase 32.

## Follow-ups (out of scope for this ADR)

- **Phase 34 (envtest)**: extend the cross-module fixture to
  drive the controller reconcile loop with the SyncJob CRD
  registered against `envtest`'s apiserver. This would pin the
  full chain Console → CR writer → admission → controller
  reconcile → metric emission. ADR-082 explicitly defers envtest
  to a separate ADR; this ADR only covers the update-path guard.
- **Helper extraction**: if a third call site needs the same
  guard (e.g. a `delete` short-circuit or a future
  `SyncJobTemplate` writer), extract a `validateCanonicalTenantID`
  helper. Not needed today.
