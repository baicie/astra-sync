# Phase 14 Design: GitOps & Progressive Delivery with ArgoCD

## Context

Phase 13 delivered production-grade Kubernetes manifests (HPA, PDB, NetworkPolicy, startup probes,
Prometheus annotations) and a hardened `values-production.yaml` profile. Day-2 operations still
require operators to manually run `helm upgrade` or CI scripts, which introduces:

- **Drift risk**: manual changes applied directly to the cluster bypass git audit trail.
- **No automated rollback**: a bad deploy requires manual `helm rollback` with no
  git-backed version history.
- **Multi-cluster complexity**: managing N clusters with separate `helm upgrade` calls
  scales poorly and is error-prone.
- **Approval gates**: production deployments lack an automated mechanism to require
  human review before applying changes.

GitOps solves all four by making git the single source of truth. ArgoCD continuously
reconciles live cluster state against git, surfaces drift, and supports one-click rollback.

## Decisions

### 1. ArgoCD over Flux (Slice 40.1)

ArgoCD is chosen over Flux v2 for three reasons:

1. **ApplicationSet model**: multi-cluster management with a single declarative manifest
   scales better than per-cluster Flux `GitRepository` + `Kustomization` pairs.
2. **Health assessment**: ArgoCD has built-in health checks for Deployment, StatefulSet,
   HPA, and PDB; Flux requires custom health probes.
3. **Ecosystem familiarity**: ArgoCD has broader adoption in the CNCF ecosystem and
   a richer UI for non-gitops-native teams.

A future ADR can cover Flux migration if needed.

### 2. Helm Chart In-Repo (Slice 40.1)

The Helm chart lives in `deployment/helm/astrasync/` within the same git repository,
not in a separate chart museum. This is the simplest bootstrap path and aligns with
ADR-040 (connector catalog deployment is also in-repo).

### 3. Single Application Manifest (Slice 40.1)

The `application.yaml` manifest is the canonical reference for production single-cluster
deployments. It uses:
- `source.path`: `deployment/helm/astrasync`
- `source.targetRevision`: `HEAD` (main branch); production operators should pin to git tags
- `source.helm.valueFiles`: `values-production.yaml`

### 4. ApplicationSet Matrix Generator (Slice 40.2)

The ApplicationSet uses a matrix of:
- **Cluster generator**: enumerates all clusters registered in ArgoCD (or filtered by label).
- **Git generator**: enumerates environment directories in the repo
  (`deployment/argocd/environments/{dev,staging,production}/`).

Each `(cluster, environment)` pair generates one Application with:
- `releaseName: "astrasync-<cluster>"` to avoid Helm release name collisions.
- `source.helm.valueFiles: [values-staging.yaml, <env>/values-override.yaml]` layered values.

### 5. Staging Values Profile (Slice 40.4)

`values-staging.yaml` is a dedicated file (not an override) providing a middle ground
between dev defaults (in `values.yaml`) and production (`values-production.yaml`):

| Property | values.yaml | values-staging.yaml | values-production.yaml |
|----------|-------------|--------------------|-----------------------|
| image tag | `latest` | `latest` | `v0.2.0` |
| replicaCount | 1 | 2 | 3 |
| autoscaling | disabled | disabled | enabled |
| mtls | disabled | disabled | enabled |
| PDB | enabled | enabled | enabled |
| NetworkPolicy | disabled | disabled | enabled |
| persistence | disabled | 10Gi | 50Gi |
| log level | INFO | DEBUG | INFO |

### 6. RBAC (Slice 40.3)

The ArgoCD service account (`astrasync-argocd`) is scoped to:
- `applications` and `applicationsets` resources only (create/read/update/delete).
- `astrasync-*` named ConfigMaps for Helm values tracking (read-only).
- No secrets access, no cluster-wide permissions.

This satisfies least privilege: ArgoCD can manage AstraSync application lifecycle
but cannot access credentials or modify unrelated resources.

## Consequences

### Positive

- Git is the source of truth; any cluster state can be reconstructed from git history.
- Automated sync eliminates drift between environments.
- Rollback is a single `argocd app rollback` command, git-revision-referenced.
- ApplicationSet scales to N clusters with one manifest update.
- ArgoCD UI provides visual diff before syncing (approval gate in practice).

### Negative

- ArgoCD server must have network access to the git repository (SSH key or HTTPS token
  in a Secret referenced by the Application/ApplicationSet).
- A bootstrapping step is required: each target cluster must be registered with
  `argocd cluster add` before the ApplicationSet can target it.
- Auto-sync on `HEAD` can cause unexpected restarts if CI pushes during a maintenance
  window. Mitigation: use git tags for production (`targetRevision: v0.2.0`) or use the
  `argocd.argoproj.io/async-hold` annotation.
- ArgoCD is an additional moving part; teams must operate and upgrade ArgoCD separately.

## Alternatives Considered

### Flux v2

Flux's GitRepository + Kustomization model is more Kubernetes-native and avoids a
central server process. However, the ApplicationSet matrix generator significantly
simplifies multi-cluster management. For teams already committed to Flux, a migration
ADR is the right vehicle.

### Helmfile

Helmfile separates the layering problem from ArgoCD by using `helmfile.yaml` to
manage multiple environments. This adds a tool dependency. Layering via multiple
`--values` flags (as done here) achieves the same result without a new tool.

### ArgoCD Rollouts

ArgoCD Rollouts adds canary/blue-green progressive delivery strategies. It requires
installing the Rollouts controller and a second reconciliation loop. Excluded from
Phase 14 scope; tracked for Phase 15.
