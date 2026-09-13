# Phase 34 — Reconcile-Metric Tenant-Id Derivation at the Controller Boundary

## Status

Active. Layer-1 tests in
`control-plane/controller/internal/controller/reconcile_tenant_id_metric_test.go`
ship five new cases. The historical
`TestReconcileObservesSuccessAndFailureOutcomes` is updated to
assert against the canonical tenant the resource carries rather
than the hard-coded `"_unknown"` placeholder. All tests pass on
`go test ./control-plane/controller/... -count=1` (10.0s).

## Goal

Phase 31/32/33 closed the **write** side of the tenant-id
envelope chain: BFF ingress canonical-UUID validation, Console
CR writer label translation, and create / update mutation
guards. Phase 34 closes the **observe** side at the Controller
boundary: the reconcile-level metric
`controller_job_controller_reconcile_duration_seconds` must
bind its `tenant_id` label to the same canonical tenant the BFF
wrote into `metadata.labels["astrasync.io/tenant-id"]`, not to
the historical hard-coded `"_unknown"`.

## What ships in Phase 34

A single-package Layer-1 test addition plus a 5-line
production-code change in
`control-plane/controller/internal/controller/syncjob_controller.go`
plus a 1-line assertion update in
`observability_test.go`.

```
control-plane/controller/internal/controller/
├── syncjob_controller.go                     # +5 lines, -1 line
├── observability_test.go                     # assertion refreshed
└── reconcile_tenant_id_metric_test.go        # +5 cases
```

## Test coverage

### New file: `reconcile_tenant_id_metric_test.go`

| Test case | Asserts |
| --- | --- |
| `canonical_uuid_passes_through` | `tenant_id="0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"` survives the defer; `outcome="success"`. |
| `missing_label_collapses_to_unknown` | `tenant_id="_unknown"` when `metadata.labels == nil`. |
| `non_canonical_uuid_collapses_to_unknown` | `tenant_id="_unknown"` for `ALICE@acme.example` (the same value the controller `Recorder.ObserveStateTransition` already rejects — see `syncjob_emission_test.go`). |
| `platform_self_scope_passes_through` | `tenant_id="_platform"` (the platform self-scope value, distinct from a UUID) flows through verbatim. |
| `empty_string_label_collapses_to_unknown` | `tenant_id="_unknown"` when the label key is present but empty. |

The five cases reuse the table-driven pattern from
`TestObserveTransitionEmitsBoundedSeries` so the controller's
two tenant-deriving Recorder methods (`ObserveStateTransition`
and `ObserveReconcile`) follow an identical
`NormalizeTenant` allowlist. The `empty_string_label` case is
unique to the reconcile defer because the historical hard-code
silently accepted empty strings as `_unknown`; the new path
makes the contract explicit and visible to dashboard recipes.

### Updated: `observability_test.go`

`TestReconcileObservesSuccessAndFailureOutcomes` now asserts:

```go
recorder.observations[0].tenantID == "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"
recorder.observations[1].tenantID == "0190f7c4-6c8d-7a01-9d2b-1ecabdff0011"
```

instead of the historical
`tenantID == "_unknown"` (both for the success and failure
outcomes). The failure path is bound to the canonical tenant
because the resource is fetched before the Jobs-nil guard
fires; the defer sees the bound tenant, not the
`Get`-failure placeholder.

## Production code change

`control-plane/controller/internal/controller/syncjob_controller.go::Reconcile`
gains a 5-line tenant-id resolution block after the `Get`
succeeds:

```go
tenantID := normalize.UnknownTenant
defer func() {
    if r.Metrics == nil { return }
    outcome := "success"
    if reconcileErr != nil { outcome = "failure" }
    r.Metrics.ObserveReconcile(tenantID, outcome, r.now().Sub(startedAt))
}()
resource := &syncv1.SyncJob{}
if err := r.Get(ctx, request.NamespacedName, resource); err != nil {
    return ctrl.Result{}, client.IgnoreNotFound(err)
}
if resource.Labels != nil {
    tenantID = normalize.NormalizeTenant(resource.Labels["astrasync.io/tenant-id"])
}
```

The label is routed through the same `normalize.NormalizeTenant`
allowlist every other tenant-deriving Recorder in the control
plane uses (ADR-058 §3). The pre-`Get` placeholder stays at
`"_unknown"` so a `Get` failure (object deleted, watch lag) is
attributed to the unknown bucket rather than leaking a stale
value. The historical hard-code
`r.Metrics.ObserveReconcile("_unknown", ...)` is removed.

## Why a per-resource label, not a per-namespace label

The metric's label allowlist is documented in
`docs/observability/metrics-catalog.md` as
`tenant_id, outcome, …`. Tenant-id is the cardinality budget
that the BFF label translation (ADR-072 / ADR-074) and the K8s
CEL validation (ADR-071 §2) protect — a per-namespace label
would re-introduce the same label-cardinality drift the
canonical-UUID allowlist exists to prevent. The `namespace`
label is already on the sibling `controller_job_state_total`
metric, so a dashboard that wants per-namespace latency can
join the two.

## Why now (and not in Phase 31)

Phase 31 deliberately kept the change set to the BFF
ingress / CR writer boundary so the test fixture
(`tests/cross-module/chain-tenant-id/`) could pin the chain
without controller-runtime envtest. Phase 34 is a pure Go
unit test (fake `client.Client`), not an envtest exercise, so
it does not require etcd binaries and stays inside the
`control-plane/controller` module's CI lane. The
envtest-backed controller reconcile regression is deferred
to a separate ADR (see Follow-ups).

## CI integration

The Phase 34 test runs under the existing controller Go
module CI lane (`go test ./control-plane/controller/...`). No
new workflow file. The change does not touch
`tests/cross-module/chain-tenant-id/`, so the
`cross-module-chain-tenant-id` workflow continues to gate only
the cross-module chain fixture.

## Non-Goals (this phase)

- **No envtest.** envtest depends on etcd binaries, which
  inflate the test binary and the CI lane. ADR-082 §Follow-ups
  defers envtest to a separate ADR; Phase 34 keeps the change
  set to the metric label and unit tests.
- **No new top-level dependency.** `observability/normalize`
  is already a controller module dependency (used by
  `internal/metrics`).
- **No new metric, no new RBAC, no new audit event.**
- **No helper extraction.** The label-extract logic stays
  inline at the single call site in `Reconcile`; helper
  extraction is a future refactor if a second metric owner
  needs the same path.
- **No `controller_job_state_total` change.** That metric
  already routes through `Recorder.ObserveStateTransition`,
  which is covered by `syncjob_emission_test.go`.

## Acceptance criteria

- [x] `reconcile_tenant_id_metric_test.go` exists with five
      table-driven cases (canonical UUID, missing label, non-
      canonical UUID, `_platform` self-scope, empty string).
- [x] `Reconcile` defer derives `tenant_id` from
      `resource.Labels["astrasync.io/tenant-id"]` after `Get`
      succeeds; the pre-`Get` placeholder stays at
      `_unknown`.
- [x] `TestReconcileObservesSuccessAndFailureOutcomes`
      asserts against the canonical tenant, not the
      hard-coded `"_unknown"` placeholder.
- [x] `go vet ./...` clean.
- [x] `go test ./...` clean in the controller module
      (`internal/controller` + `internal/metrics`).
- [x] `CHANGELOG.md` Unreleased / Changed entry summarises
      the contract.

## Follow-ups (out of scope for Phase 34)

- **envtest-backed controller reconcile regression**:
  drive the reconcile loop with the SyncJob CRD registered
  against `envtest`'s apiserver, asserting that the
  bound tenant survives the full Get → finalizer-add →
  ProjectStatus path. ADR-082 §Follow-ups.
- **Helper extraction** (`reconcileTenantFromLabels(resource)`):
  only one call site today; inline is more readable than a
  one-line helper. Defer until a second metric owner needs
  the same path.

## Rollback

Removing the 5-line `tenantID` resolution block and the new
test file reverts the slice. The metric continues to emit
`tenant_id="_unknown"` for every reconcile — the historical
degenerate single-series state. The K8s API server CEL
`XValidation` rule (ADR-071 §2) and the BFF ingress
canonical-UUID check (Phase 28-A) remain as upstream and
downstream lines of defence, exactly as before Phase 34.
