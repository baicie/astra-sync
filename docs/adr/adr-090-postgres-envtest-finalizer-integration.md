# ADR-090: PostgreSQL and Envtest Finalizer Integration

## Status

Accepted

## Context

Phase 36 verified Kubernetes finalizer mechanics with envtest. Phase 39 verified
the real controller-runtime manager and reconcile metrics. Neither test joined
the Kubernetes finalizer lifecycle to the durable PostgreSQL Job repository.

The Controller deletion path is cross-store:

- an active Job must first be moved to `CANCELING` and retained;
- a non-active Job is deleted from PostgreSQL;
- only after the durable delete succeeds is the Kubernetes finalizer removed.

A fake or in-memory repository cannot prove that this sequence works against
the production PostgreSQL adapter.

## Decision

Add a controller integration test that joins envtest to a real PostgreSQL
service:

1. The `control-plane-integration` job provides a pinned `postgres:16-alpine`
   service and exports `ASTRASYNC_TEST_POSTGRES_URL`.
2. The test opens and migrates `job.postgres.Repository`.
3. A real controller-runtime manager reconciles a running tenant-labelled
   `SyncJob` into PostgreSQL.
4. Deleting the CR moves the durable Job to `CANCELING`; the CR retains its
   deletion timestamp and finalizer while work is active.
5. Advancing the durable Job to `CANCELED` allows the Controller to delete the
   PostgreSQL row, remove the finalizer, and let Kubernetes delete the CR.

The PostgreSQL service is used instead of adding testcontainers-go to the
standalone controller module. The module therefore keeps its existing
dependency graph while the integration workflow owns the external service.

## Consequences

- Finalizer cleanup is verified against the real PostgreSQL adapter and
  Kubernetes API server in one scenario.
- Active Jobs are proven to retain both durable state and the Kubernetes
  object until execution reaches a terminal state.
- Controller integration tests now require `ASTRASYNC_TEST_POSTGRES_URL`.
  Missing PostgreSQL configuration is a hard failure, not a silent skip.
- CI performs two PostgreSQL paths: testcontainers-go for repository tests and
  a job service for controller integration tests.

## Rollback

Remove the combined integration test and the workflow PostgreSQL service/env
entry. Production controller, finalizer, and repository behavior is unchanged.
