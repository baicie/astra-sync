# Phase 14 Implementation Plan

## Overview

Phase 14 adds ArgoCD GitOps integration for declarative AstraSync Kubernetes deployments.
All changes are in `deployment/argocd/` and `deployment/helm/astrasync/`. No application
code, protobuf, or database schema changes are required.

## Slice 40.1 — ArgoCD Application Manifest (Single-Cluster)

**Files created:**
- `deployment/argocd/application.yaml`

**Implementation:**

The ArgoCD Application manifest references the Helm chart in-repo:

```yaml
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: astrasync
  namespace: argocd
spec:
  project: default
  source:
    repoURL: https://github.com/astrasync/astra-sync
    targetRevision: HEAD
    path: deployment/helm/astrasync
    helm:
      valueFiles:
        - values-production.yaml
  destination:
    server: https://kubernetes.default.svc
    namespace: astrasync-system
  syncPolicy:
    automated:
      prune: true
      selfHeal: true
      allowEmpty: false
```

Health checks are defined inline for Deployment, StatefulSet, HPA, and PDB using
Lua scripting.

**Verify:**
```bash
kubectl apply -f deployment/argocd/application.yaml --dry-run=server
argocd app get astrasync --grpc-web  # after apply
```

## Slice 40.2 — ArgoCD ApplicationSet (Multi-Cluster)

**Files created:**
- `deployment/argocd/applicationset.yaml`

**Implementation:**

The ApplicationSet uses a matrix generator combining:
1. Cluster generator: lists all clusters registered in ArgoCD
2. Git generator: lists environment directories in `deployment/argocd/environments/`

```yaml
spec:
  generators:
    - matrix:
        generators:
          - clusters:
              selector: {}
          - git:
              repoURL: https://github.com/astrasync/astra-sync
              revision: HEAD
              paths:
                - path: deployment/argocd/environments/{{path}}
```

Each `(cluster, environment)` pair generates an Application with:
- `name: "astrasync-<cluster>-<environment>"`
- `releaseName: "astrasync-<cluster>"`
- `valueFiles: [values-staging.yaml, <env>/values-override.yaml]`

**Verify:**
```bash
kubectl apply -f deployment/argocd/applicationset.yaml --dry-run=server
argocd appset list | grep astrasync
```

## Slice 40.3 — RBAC: Least-Privilege ArgoCD Service Account

**Files created:**
- `deployment/argocd/namespace.yaml` (namespace + ServiceAccount + Role + RoleBinding)

**Implementation:**

Role grants only:
- `argoproj.io/applications` and `argoproj.io/applicationsets`: full lifecycle
- `astrasync-*` named ConfigMaps: read-only (for Helm values tracking)
- No secrets, no cluster-wide permissions

**Verify:**
```bash
kubectl auth can-i create applications --as=system:serviceaccount:astrasync-system:astrasync-argocd -n argocd
# expected: yes
kubectl auth can-i get secrets --as=system:serviceaccount:astrasync-system:astrasync-argocd -n astrasync-system
# expected: no
```

## Slice 40.4 — Staging Values Profile

**Files created:**
- `deployment/helm/astrasync/values-staging.yaml`

**Implementation:**

Staging profile with medium footprint (replicaCount=2, autoscaling disabled, mtls disabled,
persistence enabled with 10Gi PVCs, PDB enabled, NetworkPolicy disabled, DEBUG logging).

**Verify:**
```bash
helm lint deployment/helm/astrasync -f deployment/helm/astrasync/values-staging.yaml
helm template astrasync deployment/helm/astrasync \
  -f deployment/helm/astrasync/values-staging.yaml > /dev/null
```

## Slice 40.5 — GitOps Onboarding Documentation

**Files created:**
- `deployment/argocd/README.md`

**Content:**
- ArgoCD install instructions
- Repository registration
- Cluster registration
- Single-cluster vs ApplicationSet usage
- Sync, rollback, and CI integration commands
- Prerequisites (metrics-server, CNI with NetworkPolicy)
- Uninstall instructions

## Testing Strategy

1. **Schema validation**: `kubectl apply --dry-run=server` on all ArgoCD CRs.
2. **Helm lint**: `helm lint` with `values-staging.yaml` passes.
3. **Helm template render**: `helm template ... -f values-staging.yaml` renders cleanly.
4. **Existing CI**: `helm lint` job continues to pass (no application code changes).
5. **Documentation**: README is readable and covers all acceptance criteria.

## Rollout Notes

- Apply ArgoCD CRs in this order:
  1. `deployment/argocd/namespace.yaml` (namespace + RBAC)
  2. `deployment/argocd/application.yaml` (single-cluster) OR
     `deployment/argocd/applicationset.yaml` (multi-cluster)
- For production, pin `targetRevision` to a git tag instead of `HEAD`.
- Use `argocd.argoproj.io/async-hold: "true"` annotation to gate production syncs
  during maintenance windows.
