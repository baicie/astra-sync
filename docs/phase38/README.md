# Phase 38 — GitOps-Managed Admission Prerequisites

## Status

**Complete.**

Phase 38 makes the SyncJob CRD and tenant-label admission policy part of
declarative ArgoCD prerequisites instead of manual cluster setup.

ADR: [ADR-088](../adr/adr-088-gitops-managed-cluster-prerequisites.md)

---

## Delivered Files

```text
deployment/operator/config/
└── kustomization.yaml

deployment/argocd/
├── prerequisites-application.yaml
└── prerequisites-applicationset.yaml

docs/adr/
└── adr-088-gitops-managed-cluster-prerequisites.md
```

---

## Prerequisite Bundle

`deployment/operator/config` renders:

- `syncjobs.sync.astrasync.io` CRD
- `astrasync-syncjob-tenant-id` ValidatingAdmissionPolicy
- `astrasync-syncjob-tenant-id` ValidatingAdmissionPolicyBinding

Verify locally:

```bash
kubectl kustomize deployment/operator/config
```

---

## ArgoCD Usage

Single cluster:

```bash
kubectl apply -n argocd -f deployment/argocd/prerequisites-application.yaml
argocd app sync astrasync-prerequisites
```

All registered clusters:

```bash
kubectl apply -n argocd -f deployment/argocd/prerequisites-applicationset.yaml
```

Sync prerequisites before the workload Application:

```bash
argocd app sync astrasync-prerequisites
argocd app sync astrasync
```

---

## Safety Properties

- Prune is disabled for CRDs and admission policies.
- Self-heal is enabled.
- Server-side apply is required.
- The workload Helm release remains separate and cannot delete the
  prerequisites during normal Application pruning.

---

## Verification

CI validates:

- The prerequisite Kustomize bundle renders.
- The Application and ApplicationSet manifests parse as YAML.
- Both point to `deployment/operator/config`.
- Automated sync has `prune: false` and `selfHeal: true`.
- The ApplicationSet uses cluster discovery and preserves resources on
  deletion.

The Phase 37 envtest suite continues to verify admission behavior.

---

## Rollback

Delete the prerequisite ApplicationSet or Application without cascading
deletion:

```bash
kubectl delete -n argocd -f deployment/argocd/prerequisites-applicationset.yaml
kubectl delete -n argocd -f deployment/argocd/prerequisites-application.yaml
```

Because prune and `preserveResourcesOnDeletion` are disabled, the CRD and
admission policy remain installed until explicitly removed.
