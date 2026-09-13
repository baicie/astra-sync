# Phase 13 Completion Record

**Status:** Complete
**Chart version:** 0.2.0
**Date:** 2026-09-07

## Slices Delivered

| Slice | Description | Files | Status |
|-------|-------------|-------|--------|
| 30.1 | PDB templates (api-server, controller, scheduler) | `templates/{api-server,controller,scheduler}/pdb.yaml` | Done |
| 30.2 | HPA templates (api-server, controller, scheduler) | `templates/{api-server,controller,scheduler}/hpa.yaml` | Done |
| 30.3 | Internal NetworkPolicy (East-West) | `templates/network-policy.yaml` | Done |
| 30.4 | Startup probes + Prometheus annotations | `templates/{scheduler,controller,worker}/*` | Done |
| 30.5 | Production values profile | `values-production.yaml` | Done |
| 30.6 | Chart version bump to 0.2.0 | `Chart.yaml` | Done |

## Verification Results

| Check | Command | Result |
|-------|---------|--------|
| Default values lint | `helm lint deployment/helm/astrasync` | Pass (0 failed) |
| Production values lint | `helm lint --values values-production.yaml` | Pass (0 failed) |
| PDB count | `helm template` | 3 PDBs (api-server, controller, scheduler) |
| HPA count | `helm template --set autoscaling.enabled=true ...` | 3 HPAs |
| NetworkPolicy | `helm template --set networkPolicy.enabled=true --set networkPolicy.internal=true` | 1 NetworkPolicy |
| Scheduler startupProbe | Render check | `httpGet /healthz, failureThreshold: 30, periodSeconds: 10` |
| Controller startupProbe | Render check | `httpGet /healthz, failureThreshold: 30, periodSeconds: 10` |
| Worker startupProbe | Render check | `tcpSocket port: worker, failureThreshold: 12, periodSeconds: 10` |
| Prometheus annotations | Render check | 3 scrape annotations (api-server, scheduler, controller) |
| Worker annotations | Render check | 1 scrape annotation with monitoring enabled |
| Chart version | `helm show chart` | version: 0.2.0, appVersion: "0.2.0" |

## Fixes Applied During Implementation

1. **Network policy template indentation bug** — `nindent 16` over-indented the
   `matchLabels` selector, causing invalid YAML. Fixed by changing to
   `nindent 14` to align with the parent `matchLabels` block at 12 spaces.
2. **Network policy `if` statement bug** — Missing `and` keyword caused both
   conditionals to evaluate as a chained method call. Fixed by adding `and`.
3. **Duplicate PDBs** — Pre-existing PDB blocks in `rbac.yaml` duplicated the
   new per-component `pdb.yaml` files. Removed the duplicates so the
   per-component templates are the canonical location (matches the design).
4. **Production values constraint alignment** — Production mode requires
   `apiServer.auth.mode=oidc`, `console.auth.mode=oidc`, `console.api.clientCert.enabled=true`,
   and `console.api.tls.secretName`. Updated `values-production.yaml` to satisfy
   these template-level validation gates.

## Prerequisites for Production Deployment

1. **metrics-server** must be installed in the cluster for HPAs to function:
   ```bash
   kubectl apply -f https://github.com/kubernetes-sigs/metrics-server/releases/latest/download/components.yaml
   ```
2. **NetworkPolicy-capable CNI** (Calico, Cilium, kube-router) required for
   the internal East-West network policy.
3. **TLS certificates** must be provisioned via cert-manager or externally and
   stored in the referenced Secrets before `apiServer.environment=production`
   is enabled.
4. **OIDC issuer** must be reachable from the cluster; configure
   `apiServer.auth.oidcIssuer` and `console.auth.oidcIssuer` with valid URLs.
