# Phase 40 - PostgreSQL and Envtest Finalizer Cleanup

## Status

**Complete.**

Phase 40 closes the Phase 36 follow-up for cross-store finalizer cleanup by
running the real controller-runtime manager against both envtest and
PostgreSQL.

ADR: [ADR-090](../adr/adr-090-postgres-envtest-finalizer-integration.md)

---

## Goal

Verify one deletion scenario across both ownership stores:

1. A running SyncJob exists in Kubernetes and PostgreSQL.
2. Deleting the CR requests durable cancellation.
3. The active Job remains in PostgreSQL and the finalizer keeps the CR present.
4. A terminal `CANCELED` state allows durable deletion.
5. PostgreSQL deletion completes before the finalizer is removed.
6. Kubernetes then deletes the CR.

---

## Delivered Files

```text
control-plane/controller/internal/controller/
└── postgres_finalizer_integration_test.go

.github/workflows/
└── control-plane-integration.yml

docs/adr/
└── adr-090-postgres-envtest-finalizer-integration.md

docs/phase40/
└── README.md
```

---

## Test Contract

`TestControllerManagerFinalizerCleanupAcrossPostgresIntegration`:

- opens and migrates the production `job.postgres.Repository`;
- starts a real controller-runtime manager with envtest;
- creates a tenant-labelled SyncJob desired to run;
- waits for PostgreSQL `INITIALIZING` and the projected finalizer/status;
- advances the durable Job to `RUNNING`;
- deletes the CR and asserts PostgreSQL `CANCELING` plus a retained finalizer;
- advances the durable Job to `CANCELED`;
- asserts both the PostgreSQL row and Kubernetes CR become absent.

---

## CI Integration

The `control-plane-integration` job now provides:

- `postgres:16-alpine` with a readiness probe;
- `ASTRASYNC_TEST_POSTGRES_URL` for the controller integration step.

The controller test does not add `testcontainers-go` to its module graph.
Repository integration tests continue to use testcontainers-go with dynamic
container ports.

---

## Acceptance Criteria

- [x] The test has `//go:build integration`.
- [x] Missing `ASTRASYNC_TEST_POSTGRES_URL` is a hard failure.
- [x] A real PostgreSQL adapter and envtest API server run together.
- [x] Active deletion retains durable state and the Kubernetes finalizer.
- [x] Terminal deletion removes both stores.
- [x] The integration workflow provides and health-checks PostgreSQL.
- [x] The CI build-tag guard includes the new test.
- [x] ADR-090 is indexed in `docs/adr/README.md`.
- [x] CHANGELOG includes the Phase 40 entry.

---

## Non-Goals

- No production reconciler or repository behavior change.
- No new Go dependency.
- No epoch-fence stale-writer simulation.
- No PostgreSQL migration change.
- No RBAC, CRD, Helm, or container-image change.

---

## Rollback

Remove the combined integration test, the workflow PostgreSQL service/env
entry, and the Phase 40 documentation/index entries. Production behavior is
unchanged.
