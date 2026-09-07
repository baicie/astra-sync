# Phase 14: GitOps & Progressive Delivery with ArgoCD

## Status

**Complete.** Phase 14 adds ArgoCD GitOps integration for declarative AstraSync
Kubernetes deployments, enabling automated sync, drift detection, and rollback across
development, staging, and production clusters. The CI pipeline now validates both
production and staging values profiles against the in-repo Helm chart.

## Goals

1. Provide ArgoCD Application manifest for single-cluster deployments.
2. Provide ArgoCD ApplicationSet manifest for multi-cluster, multi-environment deployments.
3. Establish least-privilege RBAC for the ArgoCD service account.
4. Publish a staging values profile complementary to the production profile.
5. Document GitOps onboarding and day-2 operations in the ArgoCD README.
6. Add CI validation for ArgoCD manifests and staging values.

## Non-goals

- Installing ArgoCD itself (operator responsibility; ArgoCD operator installation is
  documented in the onboarding README).
- ArgoCD Rollouts for canary/blue-green progressive delivery (Phase 15 follow-up).
- External Secrets Operator integration (tracked as separate ADR).
- Multi-tenancy with tenant-scoped ArgoCD projects (future work).

## Roadmap

| Slice | Focus | Status |
|-------|-------|--------|
| 40.1 | ArgoCD Application manifest (single-cluster, Helm) | Done |
| 40.2 | ArgoCD ApplicationSet (multi-cluster × environment matrix) | Done |
| 40.3 | RBAC: least-privilege ArgoCD service account | Done |
| 40.4 | Staging values profile (`values-staging.yaml`) | Done |
| 40.5 | GitOps onboarding documentation | Done |
| 40.6 | ArgoCD CR schema validation + staging values CI step | Done |

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| `deployment/argocd/application.yaml` renders a valid ArgoCD Application CR | Done |
| `deployment/argocd/applicationset.yaml` renders a valid ArgoCD ApplicationSet CR | Done |
| `deployment/helm/astrasync/values-staging.yaml` passes `helm lint` | Done (0 failed) |
| RBAC manifest grants only application/applicationset permissions scoped to `astrasync-*` resources | Done |
| ArgoCD README covers install, sync, rollback, and CI integration | Done |
| Existing CI jobs (unit tests, helm lint, multi-region) remain green | Done (no application code changes) |
| CI step validates staging values: 3 PDBs, 0 HPA, 0 NetworkPolicy, env marker, DEBUG level | Done |

## Production Render Verification

Three value profiles render cleanly:

| Profile | Image Tag | Replicas | Autoscaling | mTLS | NetworkPolicy | Persistence |
|---------|-----------|----------|-------------|------|---------------|-------------|
| default (`values.yaml`) | `latest` | 1 | off | off | off | off |
| staging (`values-staging.yaml`) | `latest` | 2 | off | off | off | 10Gi |
| production (`values-production.yaml`) | `v0.2.0` | 3 | on | on | on | 50Gi |

## Records

- [Design](40-gitops/README.md)
- [Implementation plan](40-gitops/implementation-plan.md)
- [Verification](40-gitops/verification.md)
- [Completion record](40-gitops/completion.md)
- [ADR-054 — ArgoCD GitOps integration](../adr/adr-054-argocd-gitops-integration.md)

## Architectural Boundary

Phase 14 changes only Kubernetes deployment topology and CI gates. It does not
modify application code, protocol buffers, database schemas, or the operator's
reconciliation logic. The ArgoCD CRs are operator-controlled: the chart stays
the source of truth for runtime manifest generation, and ArgoCD reconciles the
cluster state against the chart values declared in `values-{staging,production}.yaml`.
