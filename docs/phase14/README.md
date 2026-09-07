# Phase 14: GitOps & Progressive Delivery with ArgoCD

## Status

**In Progress.** Phase 14 adds ArgoCD GitOps integration for declarative AstraSync
Kubernetes deployments, enabling automated sync, drift detection, and rollback across
development, staging, and production clusters.

## Goals

1. Provide ArgoCD Application manifest for single-cluster deployments.
2. Provide ArgoCD ApplicationSet manifest for multi-cluster, multi-environment deployments.
3. Establish least-privilege RBAC for the ArgoCD service account.
4. Publish a staging values profile complementary to the production profile.
5. Document GitOps onboarding and day-2 operations in the ArgoCD README.

## Non-goals

- Installing ArgoCD itself (operator responsibility; ArgoCD operator installation is
  documented in the onboarding README).
- ArgoCD Rollouts for canary/blue-green progressive delivery (Phase 15 follow-up).
- External Secrets Operator integration (tracked as separate ADR).
- Multi-tenancy with tenant-scoped ArgoCD projects (future work).

## Roadmap

| Slice | Focus | Status |
|-------|-------|--------|
| 40.1 | ArgoCD Application manifest (single-cluster, Helm) | In progress |
| 40.2 | ArgoCD ApplicationSet (multi-cluster × environment matrix) | In progress |
| 40.3 | RBAC: least-privilege ArgoCD service account | In progress |
| 40.4 | Staging values profile (`values-staging.yaml`) | In progress |
| 40.5 | GitOps onboarding documentation | In progress |

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| `deployment/argocd/application.yaml` renders a valid ArgoCD Application CR | TBD |
| `deployment/argocd/applicationset.yaml` renders a valid ArgoCD ApplicationSet CR | TBD |
| `deployment/helm/astrasync/values-staging.yaml` passes `helm lint` | TBD |
| RBAC manifest grants only application/applicationset permissions scoped to `astrasync-*` resources | TBD |
| ArgoCD README covers install, sync, rollback, and CI integration | TBD |
| Existing CI jobs (unit tests, helm lint, multi-region) remain green | TBD |
