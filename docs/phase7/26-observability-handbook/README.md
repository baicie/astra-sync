# Slice 26: Observability Handbook and Dashboard Consolidation

## Summary

Slice 26 closes the Phase 7 entry criterion that ADR-044 §"Phase 7
entry criteria" §4 recorded:

> Observability consolidation. Slices 14–16 added bounded Arrow batches,
> adaptive parallelism, spillable exchange, and checkpoint metrics.
> Phase 7 unifies the SLF4J and zap logs, the Prometheus metrics, and
> the audit trail into a single operational handbook so that an
> operator can derive per-tenant SLI/SLO from a single dashboard.

The slice adds `docs/observability/`, an operator handbook that
records the metric names, log fields, audit columns, and SLI/SLO
definitions that the operator consumes through the populated
dashboard. The handbook is a documentation-only delivery; the
Java data plane migration to SLF4J and the Prometheus client
registration in the control plane Go modules are follow-up slices
that the handbook documents.

## Boundary

This slice:

- Adds `docs/observability/README.md` and the five documents that
  the README references: `metrics-catalog.md`, `log-conventions.md`,
  `audit-correlation.md`, `slo-handbook.md`, and `dashboard-recipes.md`.
- Documents the current state of the three signals: SLF4J in the
  Java data plane, zap in the controller, the standard library `log`
  in the API Server and the Console, the Prometheus client library
  in the controller, and the audit table in PostgreSQL.
- Documents the `request_id` join key that links the three signals.
- Inherits the Slice 24 CI guard (`make check-runbooks`) so the
  handbook's template-vs-populated boundary is enforced.

This slice does not:

- Migrate the Java data plane to SLF4J. The migration is a follow-up
  slice.
- Register the Prometheus client in the API Server, the Console, or
  the auth module. The registration is a follow-up slice.
- Add a Prometheus rule file. The rule file is a deployment artefact.
- Add a Grafana dashboard. The dashboard is a deployment artefact.
- Replace the Helm chart's `monitoring` and `logging` sections. The
  chart exposes the knobs the operator uses to wire the platform's
  signal store.

## Follow-up records

The Phase 7 Slice 26 follow-up slices (F1–F5) close the gap between
the documentation and the implementation. They are recorded in
[`../../observability/changelog.md`](../../observability/changelog.md)
together with their source commits. The follow-up slices satisfy
the §"Boundary / Does not" section of this README.

- F1: SLF4J + Logback + Logstash JSON foundation for the Java data
  plane (coordinator + worker).
- F2: SLF4J migration of `CoordinatorApplication`,
  `WorkerApplication`, and `ExecutionHeartbeat` error paths.
- F3: `log/slog` migration of the API Server, Console, Scheduler,
  Connection Test Executor, and the auth admin CLI.
- F4: Prometheus client registration in the API Server, Scheduler,
  Connection Test Executor, auth library, and Console, plus the
  dedicated `/metrics` HTTP listener on port `9090`.
- F5: Helm chart wire-up of `monitoring.prometheus.enabled`,
  `METRICS_LISTEN_ADDRESS`, the `metrics` Service port, and the
  `ServiceMonitor` CRD. CI render guard verifies the toggle is
  fail-closed.

## Records

- [Slice 26 Design](design.md)
- [Slice 26 Implementation Plan](implementation-plan.md)
- [Slice 26 Verification](verification.md)
- [ADR-047: Observability Handbook and Dashboard Consolidation](../../adr/adr-047-observability-handbook-and-dashboard-consolidation.md)
- [ADR-044: Phase 6 Closeout and Phase 7 Entry Criteria](../../adr/adr-044-phase6-closeout-and-phase7-entry-criteria.md)
- [ADR-020: CLI Metrics Report Contract](../../adr/adr-020-cli-metrics-report.md)
- [ADR-042: Tenant-scoped Audited Security Event Queries](../../adr/adr-042-tenant-scoped-audited-security-event-queries.md)
- [Phase 6 acceptance document](../../phase6/acceptance.md)