# ArgoCD GitOps Setup for AstraSync

This directory contains ArgoCD Application and ApplicationSet manifests that declaratively
manage AstraSync deployments across Kubernetes clusters.

## Contents

```
argocd/
  namespace.yaml          # astrasync-system namespace + service account + RBAC
  application.yaml        # Single-cluster ArgoCD Application (Helm)
  applicationset.yaml     # Multi-cluster ApplicationSet (cluster × environment matrix)
  prerequisites-application.yaml    # Single-cluster CRD + admission prerequisites
  prerequisites-applicationset.yaml # Per-cluster prerequisite ApplicationSet
  README.md              # This file
  environments/
    dev/values-override.yaml       # Dev environment overrides
    staging/values-override.yaml   # Staging environment overrides
    production/values-override.yaml # Production environment overrides
```

## Quick Start

### 1. Install ArgoCD

```bash
# Install ArgoCD in its own namespace
kubectl create namespace argocd
kubectl apply -n argocd -f https://raw.githubusercontent.com/argoproj/argo-cd/v2.13.0/manifests/install.yaml

# Wait for ArgoCD server to be ready
kubectl rollout status deployment/argocd-server -n argocd

# Port-forward to access the UI
kubectl port-forward svc/argocd-server -n argocd 8080:443
```

### 2. Register the Git Repository

```bash
# Install ArgoCD CLI
brew install argocd   # macOS; or: https://github.com/argoproj/argo-cd/releases

# Login (default password is in the argocd-initial-admin-secret)
PASSWORD=$(kubectl get secret argocd-initial-admin-secret -n argocd \
  -o jsonpath='{.data.password}' | base64 -d)
argocd login localhost:8080 --username admin --password "$PASSWORD"

# Register the repository
argocd repo add https://github.com/astrasync/astra-sync \
  --type git --name astrasync
```

### 3. Register Target Clusters (for ApplicationSet)

For each target cluster:

```bash
# Add a cluster to ArgoCD (run from a machine with kubectl access to the target cluster)
argocd cluster add <context-name> --name <short-name>

# Example: add the current cluster
argocd cluster add $(kubectl config current-context) --name in-cluster
```

### 4. Create the Namespace and Service Account

```bash
kubectl apply -f deployment/argocd/namespace.yaml
```

### 5. Apply Cluster Prerequisites

The SyncJob CRD and tenant-label admission policy are cluster-scoped
prerequisites. Apply and sync them before the workload Application:

```bash
# Single cluster
kubectl apply -n argocd -f deployment/argocd/prerequisites-application.yaml
argocd app sync astrasync-prerequisites

# Or one prerequisite Application per registered cluster
kubectl apply -n argocd -f deployment/argocd/prerequisites-applicationset.yaml
```

Prerequisite Applications disable prune and enable self-heal. They render
`deployment/operator/config`, which contains the SyncJob CRD, the
`ValidatingAdmissionPolicy`, and its binding.

The ArgoCD project/service account used for this Application must also be
allowed to manage cluster-scoped `customresourcedefinitions` and
`validatingadmissionpolicies`/`validatingadmissionpolicybindings`. These
permissions are intentionally separate from the workload release.

### 6. Option A: Single-Cluster Application

For a single cluster, apply the Application directly:

```bash
kubectl apply -n argocd -f deployment/argocd/application.yaml
```

### 6. Option B: Multi-Cluster ApplicationSet

For multiple clusters, apply the ApplicationSet:

```bash
kubectl apply -n argocd -f deployment/argocd/applicationset.yaml
```

ArgoCD will create one Application per `(cluster, environment)` pair.

## Environment Reference

| Environment | Values File | Values Override Path | Auto-sync | Description |
|-------------|-----------|---------------------|-----------|-------------|
| dev | `values-staging.yaml` + `dev/values-override.yaml` | `deployment/argocd/environments/dev/` | Enabled | Local / ephemeral dev cluster |
| staging | `values-staging.yaml` + `staging/values-override.yaml` | `deployment/argocd/environments/staging/` | Enabled | Staging / integration |
| production | `values-staging.yaml` + `production/values-override.yaml` | `deployment/argocd/environments/production/` | Enabled | Production |

To switch Application to production values directly (without ApplicationSet):

```bash
# Edit application.yaml and change values-production.yaml → values-production.yaml
# Or use the ArgoCD CLI:
argocd app set astrasync \
  --helm-value-files values-production.yaml
```

## Managing Sync

### Sync from CLI

```bash
# Sync all applications
argocd app sync astrasync

# Sync with wait (block until healthy)
argocd app sync astrasync --timeout 600

# Sync only specific resources
argocd app sync astrasync --resource ARGOCD-APP-NS/Deployment:astrasync-system:astrasync-api-server

# Watch sync progress
argocd app wait astrasync --timeout 300 --sync
```

### Rollback

```bash
# Rollback to the previous deployed revision
argocd app rollback astrasync

# Rollback to a specific revision
argocd app history astrasync   # list revisions
argocd app rollback astrasync --revision <revision-id>
```

### Pause Sync (Gate Approval)

```bash
# Require manual approval before any sync
argocd app set astrasync --sync-policy manual

# Pause a specific application (hold for a maintenance window)
argocd app set astrasync --annotation argocd.argoproj.io/async-hold="true"

# Resume
argocd app set astrasync --annotation argocd.argoproj.io/async-hold-
```

## Health Checks

ArgoCD evaluates these resource health statuses to determine overall Application health:

| Resource | Healthy condition |
|----------|-------------------|
| Deployment | `readyReplicas >= spec.replicas` |
| StatefulSet | `readyReplicas >= spec.replicas` |
| HorizontalPodAutoscaler | `ScalingActive` condition is `True` |
| PodDisruptionBudget | `disruptionsAllowed > 0` |

If any monitored resource is not healthy, the Application status shows `Degraded`.

## Drift Detection

ArgoCD detects drift by comparing the live cluster state against the git state.
Drift occurs when:

- Someone manually edits a managed resource in the cluster
- A resource is deleted from git but still exists in the cluster
- A new resource is added to git and requires creation

With `selfHeal: true`, ArgoCD automatically corrects drift on the next sync.
With `selfHeal: false` (or `selfHeal: true` but paused), drift is surfaced in the UI
but not automatically corrected.

## CI Integration

CI can trigger ArgoCD sync after a successful image build:

```bash
# Trigger sync via ArgoCD CLI (after image push)
argocd app sync astrasync \
  --revision "v$(git describe --tags --abbrev=0)" \
  --timeout 600
```

Or use the ArgoCD API:

```bash
curl -X POST https://argocd.example.com/api/v1/applications/astrasync/sync \
  -H "Authorization: Bearer $ARGOCD_TOKEN"
```

## Uninstall

To remove ArgoCD-managed resources without deleting the namespace:

```bash
# Delete the Application (ArgoCD will not cascade-delete managed resources)
kubectl delete -n argocd -f deployment/argocd/application.yaml

# Or delete the ApplicationSet
kubectl delete -n argocd -f deployment/argocd/applicationset.yaml

# Delete prerequisite Applications; CRDs and policies are preserved.
kubectl delete -n argocd -f deployment/argocd/prerequisites-applicationset.yaml
kubectl delete -n argocd -f deployment/argocd/prerequisites-application.yaml

# Remove the namespace (this WILL delete all AstraSync resources)
kubectl delete namespace astrasync-system
```

## Prerequisites

- Kubernetes 1.30+ (required by the tenant-label ValidatingAdmissionPolicy)
- ArgoCD 2.13+
- Helm 3.15+
- Git repository with read access from ArgoCD server
- For HPA: `metrics-server` addon installed (`kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml`)
- For NetworkPolicy: CNI with NetworkPolicy support (Calico, Cilium, kube-router)

## References

- [ArgoCD Documentation](https://argo-cd.readthedocs.io/)
- [ArgoCD ApplicationSet](https://argo-cd.readthedocs.io/en/stable/user-guide/application-set/)
- [ArgoCD Health Assessment](https://argo-cd.readthedocs.io/en/stable/operator-manual/health/)
- [ADR-054](https://github.com/astrasync/astra-sync/blob/main/docs/adr/adr-054-argocd-gitops-integration.md)
