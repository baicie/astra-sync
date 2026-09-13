# ADR-092: Envtest Tool Version Automation

## Status

Accepted

## Context

The controller integration workflow hard-coded:

- `setup-envtest@v0.24.1`
- Kubernetes envtest binaries `1.36.x`

Those values must remain aligned with the standalone controller module:

- the tool version follows `sigs.k8s.io/controller-runtime`
- the test binary minor follows `k8s.io/apimachinery`

A dependency update could therefore leave CI testing against binaries from a
different Kubernetes minor than the client libraries used to compile the
controller.

## Decision

Resolve both values from `control-plane/controller/go.mod` at workflow runtime:

1. `scripts/envtest-versions.py` invokes `go list -m -json` for
   `sigs.k8s.io/controller-runtime` and `k8s.io/apimachinery`.
2. `SETUP_ENVTEST_VERSION` is the exact controller-runtime module version.
3. `ENVTEST_K8S_VERSION` converts the Kubernetes module minor to the
   kubebuilder selector `1.<minor>.x`. This explicitly handles the upstream
   convention where `k8s.io/apimachinery v0.36.x` corresponds to Kubernetes
   1.36.
4. The workflow writes the resolved names to `$GITHUB_ENV`.
5. `setup-envtest` installation and asset selection consume those variables.

Script unit tests cover concatenated `go list` JSON, missing modules, invalid
versions, and the Kubernetes `0.<minor>` to `1.<minor>` mapping. A workflow
test rejects reintroducing hard-coded versions in the setup step.

## Consequences

- Renovate or Dependabot updates to controller-runtime or Kubernetes clients
  automatically update the envtest tool and Kubernetes test binaries in CI.
- Version mismatch fails during explicit resolution instead of silently using
  stale hard-coded binaries.
- The workflow no longer duplicates dependency versions.
- The resolver requires Go module metadata and therefore runs after Go setup.

## Rollback

Remove the resolver step and restore literal setup-envtest and Kubernetes
versions in the workflow. This reintroduces manual version alignment.
