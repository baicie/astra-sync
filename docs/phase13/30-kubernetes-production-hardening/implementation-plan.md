# Phase 13 Implementation Plan

## Overview

Phase 13 adds six slices. All slices modify only `deployment/helm/astrasync/` and
`docs/phase13/`. No application code, protobuf, or database schema changes are
required.

## Slice 30.1 — PodDisruptionBudget Templates

**Files modified:**
- `deployment/helm/astrasync/values.yaml` — add `compilerValidation` entry under
  `podDisruptionBudget`; keep existing `apiServer`, `controller`, `scheduler`.

**Files created:**
- `deployment/helm/astrasync/templates/api-server/pdb.yaml`
- `deployment/helm/astrasync/templates/controller/pdb.yaml`
- `deployment/helm/astrasync/templates/scheduler/pdb.yaml`

**Implementation:**

Each PDB template follows the same pattern:

```yaml
{{- if and .Values.podDisruptionBudget.enabled ($.Values.podDisruptionBudget.<component>.minAvailable) }}
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: {{ include "astrasync.componentFullname" (dict "context" . "component" "<component>") }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "astrasync.labels" . | nindent 4 }}
    app.kubernetes.io/component: <component>
spec:
  minAvailable: {{ .Values.podDisruptionBudget.<component>.minAvailable }}
  selector:
    matchLabels:
      {{- include "astrasync.selectorLabels" . | nindent 6 }}
      app.kubernetes.io/component: <component>
{{- end }}
```

Verify: `helm template . --set podDisruptionBudget.enabled=true` renders three PDBs.

## Slice 30.2 — HorizontalPodAutoscaler Templates

**Files created:**
- `deployment/helm/astrasync/templates/api-server/hpa.yaml`
- `deployment/helm/astrasync/templates/controller/hpa.yaml`
- `deployment/helm/astrasync/templates/scheduler/hpa.yaml`

**Implementation:**

Each HPA template:

```yaml
{{- if and .Values.autoscaling.enabled ($.Values.autoscaling.<component>.enabled) }}
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {{ include "astrasync.componentFullname" (dict "context" . "component" "<component>") }}
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "astrasync.labels" . | nindent 4 }}
    app.kubernetes.io/component: <component>
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment  # or StatefulSet for worker
    name: {{ include "astrasync.componentFullname" (dict "context" . "component" "<component>") }}
  minReplicas: {{ .Values.autoscaling.<component>.minReplicas }}
  maxReplicas: {{ .Values.autoscaling.<component>.maxReplicas }}
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: {{ .Values.autoscaling.<component>.targetCPUUtilizationPercentage }}
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: {{ .Values.autoscaling.<component>.targetMemoryUtilizationPercentage }}
  behavior:
    scaleUp:
      stabilizationWindowSeconds: 0
      policies:
        - type: Percent
          value: 100
          periodSeconds: 15
    scaleDown:
      stabilizationWindowSeconds: 300
      policies:
        - type: Percent
          value: 100
          periodSeconds: 15
{{- end }}
```

Note: Add `autoscaling` top-level section in values.yaml with defaults `enabled: false`
to all components, mirroring the existing `apiServer.autoscaling` block.

Verify: `helm template . --set autoscaling.apiServer.enabled=true` renders the api-server HPA.

## Slice 30.3 — Internal Network Policy

**Files modified:**
- `deployment/helm/astrasync/values.yaml` — add `internal: false` flag under
  `networkPolicy` section.

**Files created:**
- `deployment/helm/astrasync/templates/network-policy.yaml`

**Implementation:**

Single `NetworkPolicy` with `policyTypes: [Ingress, Egress]` and per-component
`podSelector` blocks. Egress block allows port 53 (DNS), 5432 (PostgreSQL), 2379 (etcd).
Ingress block uses a map of `component → allowedSources` in values.yaml.

Verify: `helm template . --set networkPolicy.enabled=true` renders the policy with
correct port rules.

## Slice 30.4 — Startup Probes + Prometheus Annotations

**Files modified:**
- `deployment/helm/astrasync/templates/scheduler/deployment.yaml`
- `deployment/helm/astrasync/templates/worker/statefulset.yaml`
- `deployment/helm/astrasync/templates/controller/deployment.yaml`

**Changes:**

1. Add to scheduler container:
   ```yaml
   startupProbe:
     httpGet:
       path: /healthz
       port: health
     failureThreshold: 30
     periodSeconds: 10
     initialDelaySeconds: 5
   ```

2. Add to worker container:
   ```yaml
   startupProbe:
     tcpSocket:
       port: worker
     failureThreshold: 12
     periodSeconds: 10
     initialDelaySeconds: 5
   ```

3. Add Prometheus scrape annotations to scheduler and worker pod template metadata:
   ```yaml
   annotations:
     prometheus.io/scrape: "true"
     prometheus.io/port: "{{ .Values.monitoring.prometheus.port }}"
     prometheus.io/path: "/metrics"
   ```
   (Merge with existing annotations — use `annotations: |` override pattern.)

4. Add Prometheus scrape annotations to controller pod template metadata (port 8081).

Verify: `helm template .` output contains `startupProbe` for scheduler and worker,
and `prometheus.io/scrape` annotations for scheduler, controller, and worker.

## Slice 30.5 — Production Values Profile

**Files created:**
- `deployment/helm/astrasync/values-production.yaml`

Content mirrors `values.yaml` with:
- `apiServer.environment: production`
- `autoscaling.<component>.enabled: true` with production replica counts
- `podDisruptionBudget.enabled: true`
- `networkPolicy.enabled: true`
- `securityContext.seccompProfile.type: RuntimeDefault`
- Resource requests equal to limits (no burst)
- Production image tag placeholder `v0.2.0`

Verify: `helm lint --strict --values deployment/helm/astrasync/values-production.yaml
deployment/helm/astrasync` passes.

## Slice 30.6 — Chart Version Bump + Helm Lint

**Files modified:**
- `deployment/helm/astrasync/Chart.yaml` — `version: 0.2.0`, `appVersion: 0.2.0`

**Verification:**
- `helm lint deployment/helm/astrasync`
- `helm template astrasync deployment/helm/astrasync --debug | head -20` (sanity)

## Testing Strategy

1. **Helm template render** — `helm template` for all combinations of
   `podDisruptionBudget.enabled`, `autoscaling.<component>.enabled`,
   `networkPolicy.enabled`.
2. **Helm lint** — `helm lint --strict` passes with both `values.yaml` and
   `values-production.yaml`.
3. **Existing CI** — `make check` continues to pass; no Java or Go source
   changes were made.

## Rollout Notes

- Network policy requires a CNI that supports `NetworkPolicy` (Calico, Cilium,
  kube-router, etc.). Document this prerequisite.
- HPA requires `metrics-server` to be installed in the cluster. Document:
  `kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml`
- Production values profile sets `apiServer.environment: production`, which enables
  the existing mTLS enforcement gate. Operators must provide TLS certificates
  before deploying.
