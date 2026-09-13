# Phase 36 — Controller Integration Tests with Envtest

## Status

**Complete.**

Phase 36 adds hermetic envtest coverage for the SyncJob Kubernetes API
surface. It validates structural CRD admission, status subresource isolation,
optimistic locking, and finalizer semantics without changing the production
reconciler.

ADR: [ADR-085](../adr/adr-085-phase36-controller-envtest-migration.md)
Correction ADR: [ADR-086](../adr/adr-086-syncjob-tenant-label-admission-correction.md)

---

## Goal

The existing `SyncJobReconciler` unit tests use a fake client. A fake client
does not enforce:

- CRD OpenAPI structural validation
- Kubernetes finalizer semantics
- `resourceVersion` optimistic-locking conflicts
- separation between `.spec` and `.status` updates

Phase 36 closes those gaps with an envtest-backed integration test package in
the existing controller module.

---

## Delivered Files

```text
control-plane/controller/internal/controller/
├── envtest_helper_test.go
└── syncjob_controller_integration_test.go

.github/workflows/
└── control-plane-integration.yml

docs/adr/
├── adr-085-phase36-controller-envtest-migration.md
└── adr-086-syncjob-tenant-label-admission-correction.md

docs/phase36/
└── README.md
```

No duplicate reconciler or production package is introduced. The integration
tests live beside the existing reconciler, while its unit tests remain
unchanged.

---

## Test Coverage

### `TestSyncJobCRDValidationIntegration`

| Case | Expected result |
| --- | --- |
| Missing delivery guarantee | Rejected as invalid |
| Malformed connector name | Rejected as invalid |
| Valid SyncJob spec | Accepted |

This validates the generated CRD structural schema. Kubernetes CRD schemas
cannot validate `metadata.labels`, so tenant-label admission enforcement is
handled by the separate correction in ADR-086.

### `TestSyncJobStatusSubresourceIntegration`

Asserts that:

- `.status` can be updated through the status subresource
- a status update does not change `metadata.generation`
- a normal spec update cannot overwrite `.status`
- a spec update advances `metadata.generation`

### `TestSyncJobOptimisticLockingIntegration`

Fetches one resource version, updates it successfully, then attempts a second
update with a stale copy. The second update must fail with a Kubernetes
`Conflict` error.

### `TestSyncJobFinalizerBlocksDeletionIntegration`

Creates a SyncJob with the control-plane finalizer, requests deletion, and
asserts that:

- the object remains visible
- `metadata.deletionTimestamp` is set
- removing the finalizer allows the API server to delete the object

---

## Envtest Setup

`envtest_helper_test.go`:

- Has build tag `//go:build integration`
- Requires `KUBEBUILDER_ASSETS`
- Loads the generated CRD from `deployment/operator/config/crd/bases/`
- Starts kube-apiserver and etcd through `controller-runtime/envtest`
- Registers `Core` and `SyncJob` schemes
- Creates an isolated default namespace
- Stops the environment with `t.Cleanup`

Missing envtest binaries are a hard failure. The helper does not use
`t.Skip`, so CI cannot report a green run while silently omitting invariant
coverage.

controller-runtime v0.24.1 does not support graceful process signaling for
envtest shutdown on Windows. The gated CI lane runs on Ubuntu; local Windows
developers should run the integration suite from Linux or WSL.

---

## CI Integration

The shared `control-plane-integration` workflow now:

1. Installs `setup-envtest@v0.24.1`.
2. Downloads Kubernetes `1.36.x` test binaries.
3. Exports their path through `KUBEBUILDER_ASSETS`.
4. Runs from the standalone controller module:

```bash
cd control-plane/controller
go test \
  -tags=integration \
  -v \
  -count=1 \
  -timeout 300s \
  ./internal/controller/...
```

The workflow path filter includes `control-plane/controller/**`, which covers
the module's source, `go.mod`, and `go.sum`.

---

## Dependencies

- `sigs.k8s.io/controller-runtime v0.24.1` was already a direct dependency of
  `control-plane/controller`.
- No production dependency is added by this phase.
- No new storage backend, checkpoint policy, CRD field, RBAC role, or metric is
  introduced.

---

## Acceptance Criteria

- [x] `envtest_helper_test.go` exists with `//go:build integration`.
- [x] Helper loads the generated SyncJob CRD and registers schemes.
- [x] Helper fails when `KUBEBUILDER_ASSETS` is missing; no `t.Skip`.
- [x] CRD validation tests cover accepted and rejected structural specs.
- [x] Status subresource tests verify spec/status/generation isolation.
- [x] Optimistic-locking test asserts a Kubernetes `Conflict`.
- [x] Finalizer test proves deletion is blocked until finalizer removal.
- [x] Tests run in the correct standalone `control-plane/controller` module.
- [x] CI installs a pinned `setup-envtest` version and exports
  `KUBEBUILDER_ASSETS`.
- [x] ADR-085 is `Accepted` and indexed in `docs/adr/README.md`.
- [x] ADR-086 records the correction to the non-installable tenant-label CEL.
- [x] CHANGELOG includes the Phase 36 entry.

---

## Non-Goals

- No PostgreSQL + envtest combined test
- No epoch-fence coordinator integration
- No production reconciler behavior change
- No new CRD field or API version
- No tenant-label admission policy or webhook in this phase
- No new Kubernetes controller
- No end-to-end cluster test

---

## Follow-ups

1. **PostgreSQL + envtest:** validate finalizer cleanup across PostgreSQL and
   Kubernetes in one scenario.
2. **Epoch fencing:** simulate a stale writer and assert ADR-006 behavior.
3. **Controller metrics:** assert reconcile metrics during a real manager run.
4. **Version automation:** keep `setup-envtest` and Kubernetes binaries aligned
   with `control-plane/controller/go.mod`.
5. **Tenant-label admission:** completed in Phase 37 with the
   ValidatingAdmissionPolicy selected by ADR-087.

---

## Rollback

Remove the two `_test.go` files, revert the Phase 36 workflow step, and restore
the previous ADR status/index entry. Production controller behavior and the
generated CRD are unaffected.
