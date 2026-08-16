# Observability Handbook

This directory is the canonical AstraSync observability handbook. The
handbook is the operator-facing companion to the production code; it
records the metric names, log fields, audit columns, and SLI/SLO
definitions that an operator needs to derive a per-tenant health
view from the three signal sources.

The handbook is the deliverable for Phase 7 Slice 26 (ADR-047). It
inherits the template-vs-populated boundary from Slice 24 (ADR-046):
the handbook is a repository artefact, the populated dashboard is a
deployment artefact.

## Signal inventory

AstraSync emits three signals. A single regression investigation
must combine all three to reach a root cause.

| Signal | Source | Format | Sample destination |
|---|---|---|---|
| Metrics | Prometheus client library calls in the control plane Go binaries | Prometheus text exposition | `monitoring.prometheus.port: 9090` (deployment-side rewrite) |
| Logs | SLF4J in the Java data plane, zap in the controller, stdlib `log` in the API Server and Console | line-delimited JSON or pattern-formatted text | Loki / stdout / deployment log store |
| Audit | PostgreSQL `audit_events` table (Slice 18) | relational rows | PostgreSQL → deployment audit store |

The data plane Java executables currently use `System.out.printf`
and `System.err.printf`. The migration to SLF4J is a follow-up slice
that the handbook's [`log-conventions.md`](log-conventions.md) §"Java
data plane" section pre-records.

## Documents

| Document | Purpose |
|---|---|
| [metrics-catalog.md](metrics-catalog.md) | Prometheus metric names, labels, and unit conventions. |
| [log-conventions.md](log-conventions.md) | Logger naming, structured fields, and log level guidance. |
| [audit-correlation.md](audit-correlation.md) | The `request_id` key that joins an audit row to a log record and a Prometheus exemplar. |
| [slo-handbook.md](slo-handbook.md) | Per-tenant SLI/SLO definitions. |
| [dashboard-recipes.md](dashboard-recipes.md) | Reference queries and Grafana panel suggestions. |

## How to populate

The handbook is a template. Operators populate it by:

1. Copying each document to the deployment-side wiki.
2. Replacing every `<placeholder>` value with the environment-specific
   value (cluster name, Prometheus job label, SLO target).
3. Removing the `<!-- placeholders: ... -->` comment at the bottom
   of each document once all placeholders are resolved.
4. Versioning the populated handbook in the deployment-side store,
   not in the AstraSync repository.

The CI guard `make check-runbooks` (added by Slice 24) enforces that
the repository copy of every document in `docs/observability/` is a
template. The guard rejects any checked-in document that lacks a
placeholder or contains a known production hostname pattern.

## Pairing with repository artefacts

The handbook cites the Phase 6 slices that emit each signal. The
cross-reference is the source of truth for an operator who needs to
trace a metric back to the code that emits it.

| Repository artefact | Handbook section that cites it |
|---|---|
| [ADR-020: CLI Metrics Report Contract](../../adr/adr-020-cli-metrics-report.md) | [metrics-catalog.md §"CLI metrics"](metrics-catalog.md) |
| [ADR-042: Tenant-scoped Audited Security Event Queries](../../adr/adr-042-tenant-scoped-audited-security-event-queries.md) | [audit-correlation.md](audit-correlation.md) |
| [Phase 6 acceptance document](../../phase6/acceptance.md) | [slo-handbook.md §"Per-slice SLIs"](slo-handbook.md) |
| [Slice 22 design](../../phase6/22-transport-hardening/design.md) | [log-conventions.md §"Security headers"](log-conventions.md) |

## Cross-reference conventions

- Every metric name is rendered as `code`.
- Every log field is rendered as `code`.
- Every audit column is rendered as `code`.
- Every ADR cross-reference is rendered as a Markdown link.

## Boundary

The handbook does not:

- Add a Prometheus rule file. The rule file is a deployment artefact;
  the operator owns the alert thresholds and the on-call rotation.
- Add a Grafana dashboard. The dashboard is a deployment artefact;
  the handbook provides reference queries that the dashboard
  consumes.
- Replace the existing Helm chart `monitoring` and `logging`
  sections. The chart exposes the knobs the operator uses to wire
  the platform's signal store; the handbook documents the
  conventions the wire must follow.
- Migrate the Java data plane to SLF4J. The migration is a
  follow-up slice; the handbook documents the convention.

The handbook does:

- Record every metric name, log field, and audit column that the
  production code currently emits.
- Provide the per-tenant SLI/SLO definitions and the reference
  queries that the operator uses to derive an SLO dashboard.
- Document the `request_id` join key that links the three signals.
- Record the data-plane SLF4J migration as a follow-up slice.

## CI guard

The handbook's template-vs-populated boundary is enforced by the
same guard that ADR-046 documented for the operational runbooks:

```sh
make check-runbooks
```

The guard walks every Markdown file under `docs/observability/` and
rejects any file that lacks a `<placeholder>` or contains a known
production hostname pattern. The guard is best-effort; it cannot
enumerate every hostname. The handbook relies on operator discipline
and code review to honour the boundary.