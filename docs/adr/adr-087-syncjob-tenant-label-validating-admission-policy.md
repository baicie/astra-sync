# ADR-087: SyncJob Tenant-Label Validating Admission Policy

## Status

Accepted — GitOps lifecycle implemented by ADR-088

## Context

ADR-086 removed the invalid metadata-level CRD CEL rule and deferred
tenant-label admission enforcement to a follow-up. Kubernetes exposes
`metadata.labels` to admission policy expressions even though CRD structural
schemas cannot validate those fields.

The supported admission options were:

- A `ValidatingAdmissionPolicy` using the built-in admission registration API.
- A controller-managed validating webhook with TLS and service lifecycle.

The built-in policy requires no new workload, certificate, or network path and
matches the cluster-level nature of the contract.

## Decision

Add a cluster-scoped `ValidatingAdmissionPolicy` and binding at
`deployment/operator/config/admission/`.

The policy:

- Matches `CREATE` and `UPDATE` for `sync.astrasync.io/v1` `SyncJob` resources.
- Requires `object.metadata.labels["astrasync.io/tenant-id"]`.
- Requires the value to match the canonical lowercase UUID pattern.
- Uses `failurePolicy: Fail` and `validationActions: ["Deny"]`.

The Console writer guards from ADR-081 and ADR-082 remain in place for early
validation and bounded metric outcomes. The admission policy is the
cluster-wide backstop for direct `kubectl` and other writers.

Kubernetes 1.30 or newer is required because the policy uses the stable
`admissionregistration.k8s.io/v1` API. Installation is intentionally separate
from the workload Helm chart: the policy is a cluster-scoped prerequisite,
like the CRDs, and must be owned by cluster administration rather than an
individual application release.

## Consequences

- SyncJobs without the required label are rejected before the controller can
  create a durable job.
- Non-canonical tenant labels are rejected at admission time.
- The controller's `_unknown` tenant metric fallback remains defensive for
  resources created before the policy or while the policy is unavailable.
- Existing SyncJobs without labels must be patched before they are updated.
- Clusters older than Kubernetes 1.30 cannot install this policy and must use
  a separately reviewed webhook implementation.
- Policy installation and upgrade ordering are explicit operational steps.

## Follow-ups

- Add policy lifecycle checks to the operator installation guide.
  **Implemented in ADR-088.**
- Decide whether ArgoCD should manage the policy as a cluster-scoped
  prerequisite in production. **Implemented in ADR-088.**
- Revisit a validating webhook only if the supported Kubernetes baseline
  drops below 1.30.

## References

- ADR-071: Tenant-Id Label on SyncJob CR
- ADR-081: Tenant-Id Label Translation at the SyncJob CR Writer
- ADR-082: Update-Mutation Tenant-Id Guard at the SyncJob CR Writer
- ADR-086: SyncJob Tenant-Label Admission Correction
- ADR-088: GitOps Management of AstraSync Cluster Prerequisites
