# ADR-086: SyncJob Tenant-Label Admission Correction

## Status

Accepted — enforcement implemented by ADR-087

## Context

ADR-071 §2 required the `astrasync.io/tenant-id` label on every SyncJob
and attempted to enforce it with a kubebuilder CEL validation rule.

Phase 36 added envtest coverage for the generated SyncJob CRD and exposed
two Kubernetes constraints:

1. A CRD cannot define `x-kubernetes-validations` under
   `openAPIV3Schema.properties.metadata`; the API server rejects anything
   other than `name` and `generateName` in that schema.
2. Moving the rule to the root schema makes the CRD syntactically valid,
   but CEL cannot reference `self.metadata.labels` because `metadata.labels`
   is not part of the structural schema exposed to CEL.

The original CRD therefore could not be installed by a Kubernetes API
server. The validation had never been effective.

## Decision

Supersede ADR-071 §2 only.

- Remove the invalid CRD-level CEL label validation from the SyncJob API type.
- Keep `astrasync.io/tenant-id` as a required SyncJob contract and document
  the canonical lowercase UUID requirement on the API type.
- Keep the trusted writer guards established by ADR-081 and ADR-082, which
  reject non-canonical tenant IDs before Console writes a SyncJob.
- Do not add a mutating or validating webhook in this correction. Admission
  enforcement must be designed separately, using either a
  ValidatingAdmissionPolicy or a validating webhook, because either option
  has deployment, compatibility, and operational consequences.
- Track admission enforcement as a follow-up rather than shipping an
  uninstallable CRD or silently claiming validation that does not exist.

## Consequences

- The generated SyncJob CRD is installable on supported Kubernetes versions.
- Before the ADR-087 policy is installed, direct `kubectl` or other writers
  can create a SyncJob without the tenant label; the controller then emits
  `_unknown` for that resource.
- Console-created SyncJobs remain protected by the Phase 32/33 writer guards.
- Operators must not treat the CRD itself as enforcing the tenant-label
  contract; ADR-087 supplies the separate admission policy.
- Phase 36 envtest tests validate structural CRD constraints, status
  subresource behavior, optimistic locking, and finalizer semantics. They do
  not claim metadata-label admission validation.

## Follow-ups

- Add a dedicated ADR for SyncJob tenant-label admission enforcement.
  **Complete in ADR-087.**
- Prefer `ValidatingAdmissionPolicy` when the supported Kubernetes baseline
  makes it available; otherwise evaluate a controller-managed validating
  webhook. **Selected and implemented in ADR-087.**
- Add migration and rollback guidance for clusters that already rely on the
  documented label contract.

## References

- ADR-071: Tenant-Id Label on SyncJob CR
- ADR-081: Tenant-Id Label Translation at the SyncJob CR Writer
- ADR-082: Update-Mutation Tenant-Id Guard at the SyncJob CR Writer
- ADR-085: Controller Integration Tests with Envtest
- ADR-087: SyncJob Tenant-Label Validating Admission Policy
