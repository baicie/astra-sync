# Phase 37 — SyncJob Tenant-Label Admission Enforcement

## Status

**Complete.**

Phase 37 restores admission-time enforcement of the
`astrasync.io/tenant-id` SyncJob label using a Kubernetes-native
`ValidatingAdmissionPolicy`.

ADR: [ADR-087](../adr/adr-087-syncjob-tenant-label-validating-admission-policy.md)

---

## Delivered Files

```text
deployment/operator/config/admission/
├── kustomization.yaml
└── syncjob-tenant-id-validating-admission-policy.yaml

control-plane/controller/internal/controller/
├── envtest_helper_test.go
└── syncjob_controller_integration_test.go

docs/adr/
└── adr-087-syncjob-tenant-label-validating-admission-policy.md
```

---

## Admission Contract

The policy matches:

- API group: `sync.astrasync.io`
- API version: `v1`
- Resource: `syncjobs`
- Operations: `CREATE`, `UPDATE`

A matching request is denied unless:

1. `metadata.labels` exists.
2. The `astrasync.io/tenant-id` key exists.
3. The value matches `^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`.

The binding uses `validationActions: ["Deny"]` and the policy uses
`failurePolicy: Fail`.

---

## Installation

Kubernetes 1.30 or newer is required.

```bash
kubectl apply -k deployment/operator/config/admission
```

The policy is cluster-scoped and intentionally outside the workload Helm
release. Cluster administrators own its lifecycle alongside the SyncJob CRDs.
For ArgoCD-managed installations, use the cluster-prerequisites Application
defined in Phase 38.

---

## Verification

`TestSyncJobTenantLabelAdmissionIntegration` installs the policy manifest into
envtest and verifies:

- A SyncJob without `astrasync.io/tenant-id` is denied.
- A non-canonical tenant value is denied.
- A canonical lowercase UUID is accepted.

The existing controller integration tests continue to verify CRD structural
validation, status subresource behavior, optimistic locking, and finalizer
semantics.

---

## Rollback

Delete the policy binding first, then the policy:

```bash
kubectl delete validatingadmissionpolicybinding astrasync-syncjob-tenant-id
kubectl delete validatingadmissionpolicy astrasync-syncjob-tenant-id
```

Rollback restores the ADR-086 state: trusted writer guards remain active, but
direct `kubectl` creation without the tenant label is no longer denied.
