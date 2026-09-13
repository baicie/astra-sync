# Phase 13 Design: Kubernetes Production Hardening

## Context

The Helm chart deployed in Phase 6 and extended through Phase 12 provides basic
Kubernetes manifests with liveness and readiness probes, resource limits, and a global
PodDisruptionBudget section in values.yaml. However, several production-critical
manifests are absent:

- **PodDisruptionBudget** templates for the HA control-plane workloads (api-server,
  controller, scheduler) are not rendered even though the values reference them.
- **HorizontalPodAutoscaler** templates are absent, preventing the API Server,
  Controller, and Scheduler from scaling under load.
- **Internal network policy** does not restrict East-West traffic between
  components, leaving a flat network assumption.
- **Startup probes** are missing on the Scheduler (a Go binary that may take
  longer to warm up) and the Worker StatefulSet (JVM startup can exceed 30s).
- **Prometheus scrape annotations** are present only on the api-server pod template;
  the scheduler and worker StatefulSet are missing them, breaking metrics collection.
- No **production values profile** exists to bundle hardened defaults (larger resource
  requests, stricter security context, autoscaling enabled).

## Decisions

### 1. PodDisruptionBudget (Slice 30.1)

Add a `templates/api-server/pdb.yaml`, `templates/controller/pdb.yaml`, and
`templates/scheduler/pdb.yaml` for each HA workload.

- Use `minAvailable` (integer) rather than `maxUnavailable` (percentage) so the
  chart is deterministic across cluster sizes.
- Add a top-level `podDisruptionBudget` section to `values.yaml` mirroring the
  existing `apiServer.minAvailable`, `controller.minAvailable`, `scheduler.minAvailable`
  fields, and add `compilerValidation.minAvailable` for completeness.
- Render PDB only when `podDisruptionBudget.enabled=true` (default `true`) so
  development environments without a disruption budget (single-node kind/minikube) can
  opt out with a single flag.
- Add a `labels` block to PDB metadata so they are selectable by Prometheus
  `PodDisruptionBudget` rules.

### 2. HorizontalPodAutoscaler (Slice 30.2)

Add HPA templates for api-server, controller, and scheduler.

- Use `autoscaling/v2` with `HorizontalPodAutoscalerSpec` supporting both CPU
  utilization and memory utilization metrics.
- Reference the Deployment/StatefulSet via `scaleTargetRef`.
- Add `behavior` block with `scaleUp.stabilizationWindowSeconds: 0` and
  `scaleDown.stabilizationWindowSeconds: 300` (5-minute cool-down) to prevent
  thrashing.
- Add `minReplicas` and `maxReplicas` to each component's values section; the
  existing `apiServer.autoscaling` block already defines these fields and will be
  reused.
- HPA is rendered only when `autoscaling.<component>.enabled=true` (default
  `false`); setting it to `true` in the production values profile activates it.
- Require `metrics-server` to be installed in the target cluster; document this in
  the production deployment runbook.

### 3. Internal Network Policy (Slice 30.3)

Add a single `templates/network-policy.yaml` covering all internal East-West traffic.

- **Ingress rules by component:**
  - `api-server`: allow from `controller`, `scheduler`, `console`, `connection-test-executor`
  - `controller`: allow from `api-server`, `scheduler`
  - `scheduler`: allow from `controller`, `api-server`
  - `compiler-validation`: allow from `api-server`
  - `connection-test-executor`: allow from `api-server`
  - `console`: allow from ingress controller (external)
  - `worker`: allow from `scheduler` (coordinator Job pod)
- **Egress rules:**
  - Allow DNS (kube-system/coredns)
  - Allow PostgreSQL (port 5432) for control-plane components
  - Allow etcd (port 2379) for control-plane components
  - Allow object storage (S3/MinIO) for worker
  - Allow external endpoints for connection tests (tenant-controlled)
- Network policy is rendered only when `networkPolicy.enabled=true` (default `false`);
  the existing `networkPolicy` section in values.yaml already defines per-component
  flags and will be extended with an `internal` flag.
- Use Kubernetes `NetworkPolicy` with `policyTypes: [Ingress, Egress]` and
  `podSelector` per component.

### 4. Startup Probes + Prometheus Annotations (Slice 30.4)

**Startup probes** are added to the scheduler Deployment and worker StatefulSet:

- Scheduler (Go): `httpGet /healthz`, `failureThreshold: 30`, `periodSeconds: 10`,
  `initialDelaySeconds: 5`. Total maximum startup time: **310s**.
- Worker (JVM): `tcpSocket` on the protocol port, `failureThreshold: 12`,
  `periodSeconds: 10`, `initialDelaySeconds: 5`. Total maximum startup time:
  **125s** (JVM warm-up).
- The existing liveness probe on the worker uses `failureThreshold: 3`, which is
  safe because the startup probe gates it.
- The existing liveness probe on the scheduler uses `failureThreshold: 3`; the
  startup probe gates it.

**Prometheus scrape annotations** are added to the scheduler pod template and the
worker StatefulSet pod template:

```yaml
prometheus.io/scrape: "true"
prometheus.io/port: "{{ .Values.monitoring.prometheus.port }}"
prometheus.io/path: "/metrics"
```

The controller pod template also gains scrape annotations (it exposes metrics on
port 8081, path `/metrics`).

### 5. Production Values Profile (Slice 30.5)

Create `deployment/helm/astrasync/values-production.yaml` with hardened defaults:

```yaml
# Production profile for AstraSync control plane.
# Activate with: helm upgrade --install astrasync astrasync \
#   --values values-production.yaml \
#   --set image.tag=v0.2.0

# Disable development defaults
apiServer:
  environment: production
  replicaCount: 3

# Enable autoscaling
autoscaling:
  enabled: true
  apiServer:
    enabled: true
    minReplicas: 3
    maxReplicas: 10
    targetCPUUtilizationPercentage: 70
    targetMemoryUtilizationPercentage: 80
  controller:
    enabled: true
    minReplicas: 3
    maxReplicas: 10
    targetCPUUtilizationPercentage: 70
  scheduler:
    enabled: true
    minReplicas: 3
    maxReplicas: 10
    targetCPUUtilizationPercentage: 70

# Enable PDB
podDisruptionBudget:
  enabled: true
  apiServer:
    minAvailable: 2
  controller:
    minAvailable: 2
  scheduler:
    minAvailable: 2

# Enable network policy
networkPolicy:
  enabled: true

# Enforce security context
securityContext:
  runAsNonRoot: true
  runAsUser: 1000
  fsGroup: 1000
  seccompProfile:
    type: RuntimeDefault

# Production resource requests match limits (no burst)
# ... (see values-production.yaml for full content)
```

### 6. Chart Version Bump (Slice 30.6)

- `Chart.yaml` `version` and `appVersion` both bumped to `0.2.0`.
- No breaking changes to the values schema; a `minor` version bump is sufficient.

## Consequences

### Positive

- Cluster operators can deploy AstraSync with guaranteed zero-downtime upgrades
  via PDB and HPA.
- Network policy reduces blast radius of a compromised workload.
- Startup probes prevent premature liveness restart of slow-starting JVM services.
- Prometheus metrics are consistently collected from all control-plane pods.
- Production values profile reduces operator misconfiguration risk.

### Negative

- Network policy may require CNI plugin support (`kubernetes.io/ingress.egress`
  `policyTypes`); Calico, Cilium, and kube-router are common compatible CNIs.
- HPA requires `metrics-server` addon; document this prerequisite in deployment.md.
- Production values profile with `environment: production` enables mTLS enforcement
  gate in the api-server; operators must provide certificates or the deployment
  will fail (by design, matching the existing validation in the api-server
  deployment template).
- Startup probe on the worker adds up to 125s of dead time before liveness
  catches genuine hangs; this is acceptable because the worker is a Job-managed
  workload and will be restarted by the Job controller on failure anyway.

## Alternatives Considered

### VPA instead of HPA

VPA adjusts resource requests automatically but requires a separate VPA CRD and
has a restart requirement. HPA is simpler for operators and matches the
existing `apiServer.autoscaling` values structure. ADR for VPA support can be
filed separately.

### Cilium NetworkPolicy

Cilium's `CiliumNetworkPolicy` is richer (L7 filtering) but ties the chart to
a specific CNI. Standard `NetworkPolicy` is portable across CNIs and sufficient
for namespace-level segmentation.

### Global security context in values.yaml

The chart already has global `securityContext` and `containerSecurityContext`
fields. The production values profile sets `seccompProfile.type: RuntimeDefault`
to opt into the RuntimeDefault seccomp profile, which is the safest default for
new deployments.
