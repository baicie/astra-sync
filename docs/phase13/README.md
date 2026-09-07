# Phase 13: Kubernetes Production Hardening

## Status

**Complete.** Phase 13 productionizes the Helm chart and Kubernetes operator
lifecycle for production-grade cluster deployments. The chart is at version
`0.2.0` with all six planned slices shipped.

## Goals

1. Add PodDisruptionBudget templates for all stateful and highly available workloads.
2. Add HorizontalPodAutoscaler templates for API Server, Controller, and Scheduler.
3. Add internal East-West network policy restricting cross-component traffic.
4. Add JVM startup probes to slow-initializing Java services.
5. Add Prometheus scrape annotations to all pod templates.
6. Publish a production values profile with hardened defaults.
7. Bump chart version to `0.2.0`.

## Non-goals

- Modifying the Docker images or Java build configuration.
- Adding a ArgoCD or Flux GitOps integration.
- Implementing VerticalPodAutoscaler (requires separate ADR).
- Changing application-level timeout or concurrency tuning.
- Adding multi-cluster federation or federation-level DNS.

## Entry Criteria

| Item | ADR | Status |
|------|-----|--------|
| Phase 12 complete (CI pipeline) | — | Complete |
| Helm chart with basic probes and resource limits | — | Complete |
| `deployment/helm/` skeleton | — | Complete |

## Roadmap

| Slice | Focus | Status |
|-------|-------|--------|
| 30.1 | PodDisruptionBudget for api-server, controller, scheduler | Done |
| 30.2 | HorizontalPodAutoscaler for api-server, controller, scheduler | Done |
| 30.3 | Internal East-West network policy | Done |
| 30.4 | Startup probes + Prometheus scrape annotations | Done |
| 30.5 | Production values profile | Done |
| 30.6 | Chart version bump + helm lint | Done |

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| PDB templates render for api-server, controller, scheduler with configurable minAvailable | Done |
| HPA templates render for api-server, controller, scheduler with CPU/memory targets | Done |
| Internal network policy restricts ingress to required ports per component | Done |
| All Java services have startup probes with failureThreshold × periodSeconds ≥ 120s | Done (scheduler 300s, controller 300s, worker 120s) |
| All pod templates carry `prometheus.io/scrape`, `prometheus.io/port`, `prometheus.io/path` annotations | Done |
| `values-production.yaml` passes `helm lint` | Done |
| `helm lint` passes on the updated chart (default + production values) | Done |
| Existing Java, Go, protocol, security, and CI jobs remain green | Done (no application code, proto, or schema changes) |

## Production Render Verification

Rendering with `values-production.yaml` produces:

| Resource | Count |
|----------|-------|
| HorizontalPodAutoscaler | 3 (api-server, controller, scheduler) |
| PodDisruptionBudget | 3 (api-server, controller, scheduler) |
| NetworkPolicy | 1 (internal East-West) |
| ServiceMonitor | 3 |
| Deployment | 5 (api-server, controller, scheduler, compiler-validation, console) |
| StatefulSet | 1 (worker, when worker.enabled=true) |

## Records

- [Design](30-kubernetes-production-hardening/design.md)
- [Implementation plan](30-kubernetes-production-hardening/implementation-plan.md)
- [Verification](30-kubernetes-production-hardening/verification.md)
- [Completion record](30-kubernetes-production-hardening/completion.md)

## Architectural Boundary

Phase 13 changes only Kubernetes manifest templates and Helm values. It does not modify
application code, protocol buffers, database schemas, or the operator's reconciliation
logic. Production rollout of the updated chart requires a Helm upgrade with the
production values profile, and enabling the autoscaler requires the `metrics-server`
addon to be installed in the target cluster.
