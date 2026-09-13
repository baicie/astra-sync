# ADR-088: GitOps Management of AstraSync Cluster Prerequisites

## Status

Accepted

## Context

ADR-087 introduced a cluster-scoped `ValidatingAdmissionPolicy` for the
SyncJob tenant label. The SyncJob CRD is also cluster-scoped. The existing
ArgoCD Applications manage only the namespace-scoped workload Helm release,
leaving prerequisites to manual `kubectl apply` commands.

That split creates two operational risks:

- A workload can be deployed before its CRD or admission policy exists.
- A routine prune can delete safety-critical cluster resources.

## Decision

Create a dedicated cluster-prerequisites Kustomize bundle at
`deployment/operator/config`. It contains:

- `syncjobs.sync.astrasync.io` CRD
- SyncJob tenant-label `ValidatingAdmissionPolicy`
- `ValidatingAdmissionPolicyBinding`

Expose it through two ArgoCD entry points:

- `prerequisites-application.yaml` for a single cluster
- `prerequisites-applicationset.yaml` for all clusters registered in ArgoCD

The prerequisite Applications:

- Sync `deployment/operator/config`.
- Use server-side apply.
- Enable self-heal.
- Disable prune at both the policy and sync-option levels.
- Declare a lower sync wave for app-of-apps installations.

The workload Helm Application remains separate. Operators must sync the
prerequisite Application before the workload Application.

## Consequences

- CRD and admission-policy lifecycle become declarative in GitOps.
- Deleting or updating the workload Application cannot prune prerequisites.
- Cluster-wide ownership remains explicit and independent from any one
  workload release.
- The prerequisite Application must be granted cluster-scoped sync
  permissions by ArgoCD project policy.
- Existing installations must apply the prerequisite Application before
  enabling automated workload sync.

## Alternatives considered

### Add CRDs and policies to the workload Helm chart

**Rejected.** Helm release ownership is namespace-oriented and pruning the
release would risk cluster-scoped safety controls.

### Keep manual prerequisite installation

**Rejected.** Manual installation can drift and allows workloads to start
before admission enforcement exists.

### Use a validating webhook

**Rejected.** The native policy requires no additional workload, certificate,
or network boundary.

## References

- ADR-054: ArgoCD GitOps Integration
- ADR-087: SyncJob Tenant-Label Validating Admission Policy
