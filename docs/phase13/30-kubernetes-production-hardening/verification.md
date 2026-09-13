# Phase 13 Verification

## Verification Criteria

| Criterion | Method | Expected |
|-----------|--------|----------|
| PDB templates render for api-server, controller, scheduler | `helm template --set podDisruptionBudget.enabled=true` | 3 PDB YAML blocks |
| HPA templates render when enabled | `helm template --set autoscaling.apiServer.enabled=true` | 1 HPA YAML block for api-server |
| Network policy renders with correct port rules | `helm template --set networkPolicy.enabled=true` | Policy with ingress port 8080, 8081, 50051, 50052 and egress 53, 5432, 2379 |
| Startup probe on scheduler | `helm template` output | `startupProbe` block with `httpGet /healthz`, `failureThreshold: 30`, `periodSeconds: 10` |
| Startup probe on worker | `helm template` output | `startupProbe` block with `tcpSocket`, `failureThreshold: 12`, `periodSeconds: 10` |
| Prometheus annotations on scheduler pod | `helm template` output | `prometheus.io/scrape: "true"`, `prometheus.io/port`, `prometheus.io/path` |
| Prometheus annotations on worker pod | `helm template` output | Same scrape annotations |
| Prometheus annotations on controller pod | `helm template` output | Same scrape annotations |
| `values-production.yaml` passes helm lint | `helm lint --strict` | BUILD SUCCESS |
| Chart version bumped to 0.2.0 | `helm show chart` | version: 0.2.0, appVersion: 0.2.0 |
| `make check` remains green | `make check` | All Go vet + Spotless pass |

## Commands

```bash
# Template render checks
helm template astrasync deployment/helm/astrasync \
  --set podDisruptionBudget.enabled=true \
  --set autoscaling.apiServer.enabled=true \
  --set networkPolicy.enabled=true \
  | grep -E 'kind: (PodDisruptionBudget|HorizontalPodAutoscaler|NetworkPolicy)'

# Helm lint
helm lint --strict deployment/helm/astrasync

# Chart metadata
helm show chart deployment/helm/astrasync

# All checks
make check
```
