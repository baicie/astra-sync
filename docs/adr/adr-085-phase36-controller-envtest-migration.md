# ADR-085: Phase 36 — Controller Integration Test Migration to Envtest

## Status

Accepted

## Context

ADR-082 §Follow-ups and ADR-084 §Follow-ups identified a gap in the
controller integration test coverage: the existing
`control-plane/controller/internal/controller/` package lacks hermetic tests
that exercise Kubernetes API semantics for the SyncJob CRD.

Unit tests with fake clients cannot validate:

- **OpenAPI validation**: CRD constraints are not enforced by fake clients
- **Finalizer blocking**: Deletion is not blocked until finalizers are removed
- **ResourceVersion conflicts**: Optimistic locking is not simulated
- **Status subresource isolation**: Fake clients do not enforce the API server's
  separation between `.spec` and `.status` updates

Phase 35 (ADR-084) established the pattern for Go integration tests:

- Build tag `//go:build integration` separates unit tests from integration tests
- A shared helper encapsulates infrastructure setup
- A dedicated CI lane runs integration tests only when relevant paths change
- `t.Cleanup` ensures hermetic teardown

Phase 36 applies the same pattern to the controller, using **envtest**
(controller-runtime's embedded Kubernetes API server + etcd).

## Decision

### 1. Envtest helper — `internal/controller/envtest_helper_test.go`

Create `control-plane/controller/internal/controller/envtest_helper_test.go`
with build tag `//go:build integration`. The helper exports:

```go
func startEnvtest(t *testing.T) client.Client
```

The helper:

- Loads the SyncJob CRD from `deployment/operator/config/crd/bases/`
- Starts kube-apiserver + etcd via `envtest.Environment`
- Returns a controller-runtime client with Core and SyncJob schemes registered
- Registers `t.Cleanup` to stop the environment
- Fails fast when `KUBEBUILDER_ASSETS` is missing; it never skips the tests

The helper lives in the existing `internal/controller` package. No second
reconciler or placeholder production package is introduced.

### 2. Controller integration tests

Create
`control-plane/controller/internal/controller/syncjob_controller_integration_test.go`
with build tag `//go:build integration`. The file validates:

1. **Create path**: SyncJob CR creation passes structural CRD schema validation
2. **Status subresource**: `.status` updates do not alter `.spec` or
   `metadata.generation`
3. **Optimistic locking**: Concurrent updates with a stale resourceVersion are rejected
4. **Finalizer blocking**: A CR with a finalizer remains until the finalizer is removed

These tests validate **Kubernetes API semantics only**. Existing
`internal/controller` unit tests continue to cover reconciler behavior with
fake clients. Full reconciler integration with PostgreSQL cleanup and epoch
fencing remains deferred to future phases.

Envtest also exposed that the metadata-level CEL validation introduced by
ADR-071 could not be installed by a Kubernetes API server. ADR-086 removes
that invalid rule and preserves tenant-label enforcement as a separate
trusted-writer and admission-policy follow-up.

### 3. CI lane

Extend `.github/workflows/control-plane-integration.yml`:

```yaml
- name: Setup envtest
  run: |
    go install sigs.k8s.io/controller-runtime/tools/setup-envtest@v0.24.1
    assets_path="$(setup-envtest use -p path 1.36.x)"
    echo "KUBEBUILDER_ASSETS=${assets_path}" >> "${GITHUB_ENV}"

- name: Run controller integration tests
  run: |
    cd control-plane/controller
    go test -tags=integration -v -count=1 -timeout 300s \
      ./internal/controller/...
```

The path filter adds `control-plane/controller/**`, which covers that
standalone module's source, `go.mod`, and `go.sum`.

The job timeout increases from 10 minutes to 15 minutes because
testcontainers-go and envtest run in the same lane.

### 4. Module dependency

`sigs.k8s.io/controller-runtime v0.24.1` is already a direct dependency of
the standalone `control-plane/controller` module. Phase 36 adds no production
dependency.

Version rationale:

- v0.24.1 is the controller-runtime version already used by the controller module
- `setup-envtest` v0.24.1 ships with that controller-runtime release
- Kubernetes 1.36.x matches `k8s.io/apimachinery v0.36.3`

## Consequences

### Positive

- **ADR validation**: Phase 36 tests validate ADR-029 (finalizer contract),
  ADR-031 (optimistic locking), and SyncJob structural CRD validation
- **Hermetic**: envtest runs locally with downloaded API server binaries and
  needs no external cluster
- **Fast**: envtest startup is substantially faster than provisioning a kind cluster
- **CI gated**: Integration tests run only when relevant controller or workflow
  paths change
- **Symmetry with Phase 35**: Same build tag, helper, teardown, and CI pattern

### Negative

- **Binary dependency**: CI must install `setup-envtest` and download
  kube-apiserver + etcd test binaries
- **Version skew risk**: `setup-envtest` and Kubernetes test binaries must stay
  aligned with the controller module's Kubernetes libraries
- **Windows teardown limitation**: controller-runtime v0.24.1 cannot gracefully
  signal envtest processes on Windows; the gated suite runs on Ubuntu

### Neutral

- **No production code change**: Phase 36 adds integration tests only; the
  existing reconciler and unit tests are unchanged
- **PostgreSQL integration deferred**: Combined PostgreSQL + envtest coverage is
  deferred to a future ADR

## Alternatives considered

### Alt 1: Skip envtest and rely on e2e tests only

**Rejected**. E2E tests require a full cluster and provide slower feedback for
CRD and API semantics.

### Alt 2: Use kind in CI

**Rejected**. kind requires Docker and cluster provisioning. Envtest is faster
and sufficient for API semantic validation.

### Alt 3: Combine PostgreSQL + envtest in Phase 36

**Rejected**. Combining testcontainers-go and envtest increases infrastructure
complexity and is outside this phase's API-semantics boundary.

### Alt 4: Use the controller-runtime fake client

**Rejected**. The fake client does not enforce CRD validation, finalizer
blocking, status subresource isolation, or resourceVersion conflicts.

## Follow-ups

- **Full reconciler integration**: Combine PostgreSQL (testcontainers-go) and
  envtest to validate finalizer cleanup across both persistence boundaries.
- **Epoch fence integration test**: Simulate stale worker writes and validate
  ADR-006 fencing with a coordinator test double.
- **Controller metrics integration test**: Validate Prometheus emissions during
  a real reconcile cycle.
- **Tenant-label admission enforcement**: implemented by ADR-087.
- **Envtest version automation**: Keep `setup-envtest` and Kubernetes test
  binaries aligned through dependency automation.

## References

- ADR-006: Epoch Fencing
- ADR-029: Durable Desired-state Job Lifecycle
- ADR-031: PostgreSQL Lifecycle Convergence and Execution Liveness
- ADR-084: Phase 35 — Testcontainers-Go Migration
- ADR-086: SyncJob Tenant-Label Admission Correction
- [controller-runtime envtest](https://book.kubebuilder.io/reference/envtest.html)
- [setup-envtest documentation](https://pkg.go.dev/sigs.k8s.io/controller-runtime/tools/setup-envtest)
