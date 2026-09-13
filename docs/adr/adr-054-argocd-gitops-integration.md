# ADR-054: ArgoCD GitOps Integration

## Status

Accepted

## Context

AstraSync's Helm chart (Phase 13) provides production-grade Kubernetes manifests with HPA,
PDB, NetworkPolicy, startup probes, and a hardened production values profile. Day-2
operations still require operators to run `helm upgrade` manually or through scripts,
which introduces risk of drift, missed syncs, and no automated rollback on failure.

GitOps (ArgoCD) addresses this by making the git repository the single source of truth
for deployed state. ArgoCD continuously reconciles the live cluster against the desired
state in git, surfacing drift, enforcing approval gates, and enabling one-click rollback.

## Decision

Adopt ArgoCD as the recommended GitOps layer for AstraSync Kubernetes deployments.

### ArgoCD Application Structure

```
deployment/
  argocd/
    namespace.yaml         # astrasync-system namespace (optional, for isolated install)
    application.yaml       # Single-cluster ArgoCD Application pointing to Helm chart
    applicationset.yaml   # Multi-cluster ApplicationSet (clusters + environments matrix)
    rbac.yaml             # ArgoCD RBAC role binding for astrasync service account
    README.md             # Onboarding guide
  helm/astrasync/
    values-staging.yaml    # Staging profile (new, Phase 14)
    values-production.yaml # Production profile (added Phase 13)
```

### Application Configuration

- `source.repoURL`: `https://github.com/astrasync/astra-sync` (or self-hosted equivalent)
- `source.path`: `deployment/helm/astrasync` (Helm chart in-repo)
- `source.targetRevision`: `HEAD` (main branch), pinning to git tag for production
- `source.helm.valueFiles`: environment-specific values file (`values-staging.yaml` /
  `values-production.yaml`)
- `syncPolicy.automated`: `prune=true`, `selfHeal=true`, `allowEmpty=false`
- `syncPolicy.retry`: exponential backoff up to 5 attempts
- `ignoreDifferences`: `.metadata.annotations` on StatefulSet (ArgoCD server-side apply
  annotation), `.metadata.managedFields` (server-side field manager)

### ApplicationSet Matrix Strategy

```yaml
generators:
  - matrix:
      generators:
        - clusters: {}          # reads ClusterConfig from ArgoCD ConfigManagementPlugin
        - git:
            repoURL: https://github.com/astrasync/astra-sync
            revision: HEAD
            paths:
              - deployment/argocd/environments/{{name}}/
```

Each environment directory contains a `values-override.yaml` that ARGO_VALUES_OVERRIDE
references. Environments: `dev`, `staging`, `production`.

### RBAC

- Service account: `astrasync-argocd` in namespace `astrasync-system`
- Role: `astrasync-apps` with permissions `applications,applicationsets get/create/update`
  scoped to `astrasync-*` resources only (least privilege; no secrets, no cluster-wide)
- ArgoCD built-in role `argocd-application-controller` is NOT used for day-2 management

### Health Checks

ArgoCD health checks are defined for:
- `Deployment`: `.status.readyReplicas >= .spec.replicas`
- `StatefulSet`: same as Deployment
- `HorizontalPodAutoscaler`: `.status.conditions[?(@.type=='ScalingActive')].status == 'True'`
- `PodDisruptionBudget`: `.status.disruptionsAllowed > 0 || .status.currentHealthy >= .spec.minAvailable`

Health assessment returns `Healthy` when all application resources are healthy,
`Progressing` during sync, and `Degraded` if any resource fails.

## Consequences

### Positive

- Cluster state is always auditable and revertable to any prior git commit.
- Automated sync eliminates manual `helm upgrade` drift.
- ArgoCD UI gives operators a live diff view before syncing changes.
- Multi-cluster management via ApplicationSet scales to N clusters with a single manifest.
- Approval gates can be enforced via ArgoCD RBAC (application sync requires `argocd-admin`
  role approval for production).

### Negative

- ArgoCD must be installed in the cluster (not included in this ADR; separate
  installation via `argo cd install` or operator).
- ArgoCD server must have network access to the git repository.
- Auto-sync may cause unexpected restarts if git is updated during a maintenance window;
  operators should disable auto-sync before scheduled maintenance.
- The ApplicationSet generator requires each target cluster to be registered in ArgoCD
  (via `argocd cluster add` or theClusters Secret), which is a bootstrapping step.

## Alternatives Considered

### Flux instead of ArgoCD

Flux v2 (GitOps Toolkit) is more Kubernetes-native and does not require a dedicated
server process. However, ArgoCD provides a richer UI, application-level health checks,
and a more mature multi-cluster ApplicationSet model. For teams already using ArgoCD,
this is the lower-friction choice. A future ADR can cover Flux migration if needed.

### External Secrets Operator

ESOs would handle secret rotation separately from GitOps. Phase 14 scope is limited
to ArgoCD application management; ESO is tracked as a follow-up ADR.

### ArgoCD Rollouts for Progressive Delivery

ArgoCD Rollouts (progressive delivery via blue-green or canary) adds rollout strategy
selection to the Application spec. It requires the ArgoCD Rollouts controller to be
installed separately and introduces a second reconciliation loop. Excluded from Phase 14
scope; tracked for Phase 15.
