# Slice 05: Phase 0 Hardening

## Status

Verified; implementation and acceptance evidence archived.

## Objective

Make the Phase 0 path operable and auditable: callers can cooperatively cancel a bounded job,
automation can consume stable JSON metrics, resource and partial-result behavior is regression
tested, and checked-in examples/release evidence describe the honest delivery guarantees.

## Included

- Cooperative cancellation token and `CANCELLED` failure stage in the synchronous Engine.
- CLI `--metrics text|json` reports with stable counters, stage, and exit code 5 for cancellation.
- Resource-closure and cancellation tests for source, sink, JDBC transactions, and partial counts.
- A Phase 0 example index and runnable command matrix for CSV and H2-backed JDBC tests.
- Final acceptance, dependency, packaging, coverage, and clean-worktree evidence.

## Excluded

- Forced thread interruption, asynchronous execution, signal handlers, executors, or distributed
  cancellation propagation.
- Checkpoints, retries, recovery, idempotent replay, atomic output publication, and exactly-once.
- Persistent metrics storage, metrics endpoints, dashboards, Prometheus/OpenTelemetry integration,
  or schema-version negotiation for reports.
- New connector features, schema migration, CDC, parallel reads, and production service setup.

## Acceptance

1. A never-cancelled token preserves all existing Slice 01-04 behavior and metrics.
2. Cancellation before open creates/opens no connector; cancellation at a pull/write boundary
   reports `CANCELLED`, partial counters, and closes every opened resource in reverse order.
3. CLI text output remains compatible; `--metrics json` emits one valid object with no connector
   option value, path, SQL, password, or stack trace.
4. JSON success and failure reports include stable status/category/stage/counter fields, and
   cancellation exits with code 5 while codes 0/2/3/4 retain their meanings.
5. CSV and JDBC resource tests cover close failures, rollback/partial commits, cancellation, and
   bounded batches without an external production service.
6. Examples identify prerequisites, create-new output behavior, H2 test-only usage, and the
   at-most-once/partial-write limitation.
7. Focused, full Reactor, formatting, dependency, packaged artifact, JSON validity, and clean
   worktree checks pass.

## Records

- [Design](design.md)
- [Implementation plan](implementation-plan.md)
- [Verification](verification.md)
- [ADR-019](../../adr/adr-019-cooperative-cancellation.md)
- [ADR-020](../../adr/adr-020-cli-metrics-report.md)
