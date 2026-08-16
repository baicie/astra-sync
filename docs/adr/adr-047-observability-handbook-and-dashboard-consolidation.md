# ADR-047: Observability Handbook and Dashboard Consolidation (Phase 7 Slice 26)

## Status

Accepted (F1–F5 foundation complete; business instrumentation deferred).
Implements Phase 7 Slice 26 and closes the documentation and exposition
portion of the "Observability consolidation" entry criterion recorded by
ADR-044 §"Phase 7 entry criteria" §4. The landed logging, descriptor,
endpoint, and Helm work is recorded in
[`../observability/changelog.md`](../observability/changelog.md). Business
metric observations and Prometheus exemplars are explicitly not complete.

## Context

ADR-044 lists four Phase 7 entry criteria. Slice 23 closed the first
(ADR-045), Slice 24 closed the second (ADR-046). Slice 26 closes the
fourth:

> Observability consolidation. Slices 14–16 added bounded Arrow batches,
> adaptive parallelism, spillable exchange, and checkpoint metrics.
> Phase 7 unifies the SLF4J and zap logs, the Prometheus metrics, and
> the audit trail into a single operational handbook so that an
> operator can derive per-tenant SLI/SLO from a single dashboard.

The current state is unconsolidated:

- **Logs.** The Go control plane mixes the standard library `log`
  package (in `api-server`), the `go.uber.org/zap` package (in
  `controller`), and ad-hoc `System.out.printf` calls (in the data
  plane Java executables). Logback and SLF4J are configured as
  dependencies but unused in the engine module. The Helm chart exposes
  a single `logging.pattern` value but no per-component logger naming
  guidance.
- **Metrics.** Prometheus support is wired into the Helm chart
  (`monitoring.prometheus.enabled`, `serviceMonitor`) but the
  `serviceMonitor` defaults to disabled. The CLI metrics report
  contract (ADR-020) covers `stdout`/`stderr` summaries; the
  Prometheus metric names and labels are not catalogued.
- **Audit.** The audit table is structured (Slice 18, ADR-042) and
  records every authenticated mutation. The audit events have a
  `request_id` field but no documented link to the matching log
  record or Prometheus exemplar.

The result is that an operator looking at a regression cannot
correlate a control-plane error in the logs with a Prometheus metric
spike and an audit event without reverse-engineering the link. The
handbook is the missing piece.

## Decision

### One handbook under `docs/observability/`

The repository adds a new top-level `docs/observability/` directory
that holds the operator handbook. The handbook is a documentation
deliverable; it does not change production code, the Helm chart, or
the Prometheus rule file. The handbook is the single entry point for
an operator who needs to derive an SLO from a dashboard.

The handbook has five documents:

| Document | Purpose |
|---|---|
| `README.md` | Index, signal inventory, glossary. |
| `metrics-catalog.md` | Existing Prometheus metric names, labels, and unit conventions. The catalog is the source of truth for metric naming. |
| `log-conventions.md` | Logger naming, structured fields, log level guidance, and the rule that the data plane Java executables migrate to SLF4J through a follow-up slice. |
| `audit-correlation.md` | The `request_id` key that links an audit row to a log record and a Prometheus exemplar. The doc also covers the manual lookup procedure for an operator with no service-mesh tracing. |
| `slo-handbook.md` | Per-tenant SLI/SLO definitions: availability, freshness, deliverability, and audit completeness. The handbook is a template; the populated values are deployment-owned. |
| `dashboard-recipes.md` | Prometheus queries and Grafana panel suggestions for each SLI. The recipes are reference queries; the populated dashboards are deployment-owned. |

The handbook is a documentation-only delivery. The follow-up slice
that migrates the Java executables to SLF4J is out of scope for
Slice 26; the handbook documents the migration as a follow-up.

### Boundary between the repository handbook and the deployment dashboard

The repository handbook is the source of truth for metric names, log
fields, and audit columns. The deployment dashboard is a Grafana
manifest that consumes the handbook's reference queries. The split
mirrors ADR-046's template-vs-populated-runbook boundary:

- The handbook is parameterised by `<placeholder>` values where the
  operator must supply environment-specific values (cluster name,
  Prometheus job label, SLO targets).
- The populated dashboard is a deployment-owned artefact. The
  repository does not store a Grafana manifest.

The CI guard added by Slice 24 (`scripts/check-runbook-templates.py`)
rejects populated runbooks that contain production hostname patterns.
The same guard applies to the dashboards indirectly: the handbook
templates are populated by the operator in the deployment-side wiki,
not by the repository.

### Migration of the Java data plane to SLF4J

The handbook's `log-conventions.md` documents the rule that the Java
data plane loggers use SLF4J with a logger name that matches the
fully qualified class name. The current state is that the Java
executables (Coordinator, Worker, etc.) use `System.out.printf` and
`System.err.printf`. The migration is a follow-up slice because it
changes the runtime contract of the data plane.

The handbook records the migration as a follow-up so the boundary
between the documentation-only Slice 26 and the code-migration
follow-up stays clear. The repository does not ship the migration in
Slice 26.

### Verification

The slice is verified by:

- Reading the handbook against the existing Prometheus metric
  surface, the existing SLF4J/zap/slog logger conventions, and the
  existing audit-event schema. The handbook must classify metric names as
  emitted, descriptor-only, or reserved and must not equate registration
  with business observations.
- Reading the SLO handbook against the Phase 6 acceptance
  document. The acceptance document records the SLIs that the
  Phase 6 slices are responsible for; the SLO handbook must inherit
  those SLIs.
- Running the existing `make check-runbooks` against the handbook
  templates. The templates must satisfy the placeholder and
  hostname requirements.

## Consequences

- `docs/observability/` is the canonical location for the
  observability deliverables. An operator looking for a metric name
  or a log field convention goes there, not to the Helm chart values
  or to the code.
- The Phase 7 README updates the Slice 26 row from Design to
  Implementation Complete. Slice 25 (multi-region) is not affected.
- F1/F2 install SLF4J + Logback JSON output and migrate Coordinator/Worker
  error paths. Stable CLI and liveness summaries remain on
  `stdout`/`stderr` by compatibility contract.
- F3 uses module-local `slog` JSON logger constructors in the migrated Go
  executables; no cross-module helper or local `replace` is introduced.
- F4/F5 register Go metric descriptors, expose optional `/metrics`
  listeners, and wire Helm discovery. Business observations, request
  exemplars, and Java data-plane metric families remain future work.
- The audit-correlation document becomes a pre-requisite for any
  log-side or metric-side change. Future slices that add a log
  field or a metric label must update the corresponding document
  in `docs/observability/`.

## Alternatives considered

- **Ship a Prometheus rule file alongside the handbook.** Rejected.
  The rule file is a deployment-owned artefact; the operator owns
  the alert thresholds and the on-call rotation. The handbook
  provides reference queries; the populated rule file lives in the
  deployment-side repository.
- **Migrate the Java data plane to SLF4J as part of Slice 26.**
  Rejected. The migration is a data-plane runtime change that
  affects the build of every Java artefact. The handbook can
  document the convention without changing the runtime in the same
  slice. The migration is a follow-up slice that adds the logger
  calls and the SLF4J + Logback configuration.
- **Replace the Helm chart's `logging` section with a structured
  logging sidecar.** Rejected. The structured logging sidecar is a
  deployment concern; the Helm chart already exposes the
  `logging.pattern` knob for the operator to override.
- **Add tracing to the handbook.** Deferred. Service-mesh tracing
  (Jaeger, Tempo, OpenTelemetry Collector) is a deployment-side
  choice; the handbook defers tracing to a future slice when a
  concrete deployment picks a tracing backend.
