# Slice 26 Design

## Scope

Slice 26 ships an operator-facing observability handbook under
`docs/observability/`. The handbook is the single entry point for
an operator who needs to derive a per-tenant SLI/SLO from a
dashboard. The slice is documentation-only; the handbook documents
the conventions and the follow-up migrations that the production
code will adopt.

## Handbook selection

The handbook has five documents. Each document covers a single
concern. The five documents are paired with the three signals that
the AstraSync control plane and data plane emit.

| Document | Concern | Signal |
|---|---|---|
| `metrics-catalog.md` | Metric names, labels, and unit conventions. | Prometheus |
| `log-conventions.md` | Logger naming, structured fields, and log level guidance. | Logs |
| `audit-correlation.md` | The `request_id` join key. | Audit |
| `slo-handbook.md` | Per-tenant SLI/SLO definitions. | All three |
| `dashboard-recipes.md` | Reference queries for the populated dashboard. | All three |

The handbook mirrors the
[ADR-020: CLI Metrics Report Contract](../../adr/adr-020-cli-metrics-report.md)
shape for the CLI metrics. The CLI emits a JSON object per run;
the handbook records the shape so the operator can consume the JSON
without scraping the human-readable text.

## Boundary between the repository handbook and the deployment dashboard

The handbook is a template. The populated dashboard is a
deployment-owned artefact. The split mirrors the Slice 24
template-vs-populated boundary:

- The handbook is parameterised by `<placeholder>` values where the
  operator must supply environment-specific values (cluster name,
  Prometheus job label, SLO targets).
- The populated dashboard is a Grafana manifest that consumes the
  handbook's reference queries. The repository does not store the
  manifest.

The Slice 24 CI guard (`make check-runbooks`) rejects populated
handbook documents that contain production hostname patterns. The
guard applies to `docs/observability/` because the handbook shares
the same template-vs-populated boundary.

## Java data plane migration

The handbook's `log-conventions.md` §"Follow-up migration" section
records the rule that the Java data plane loggers use SLF4J with a
logger name that matches the fully qualified class name. The F1/F2
follow-up now routes Coordinator and Worker error paths through SLF4J;
stable CLI summaries and liveness output intentionally retain their
`System.out.printf`/`System.err.printf` boundary.

The follow-up slice is expected to:

1. Route production error paths through SLF4J `Logger` calls while
   preserving the stable CLI and liveness output contract.
2. Add the Logback configuration that emits the line-delimited
   JSON layout by default.
3. Register the Prometheus client in the Java module and emit the
   metrics that the [`metrics-catalog.md`](../../observability/metrics-catalog.md)
   document records.

The handbook documents the migration but does not implement it. The
boundary between the documentation-only Slice 26 and the
code-migration follow-up stays clear.

## Prometheus client registration

The F4 follow-up declares the Prometheus client directly in the API
Server, Scheduler, Connection Test Executor, and Console modules and
adds descriptor packages. The auth module contains a descriptor package,
but the one-shot admin CLI does not import it or expose a listener. The
long-running executables bind `/metrics` only when
`METRICS_LISTEN_ADDRESS` is non-empty. The remaining instrumentation work
is expected to:

1. Add the Prometheus client as a direct dependency.
2. Register and observe the metrics that the
   [`metrics-catalog.md`](../../observability/metrics-catalog.md)
   document records.
3. Configure bounded Prometheus exemplars at business call sites; merely
   using the default registerer does not enable them. The
   [`audit-correlation.md`](../../observability/audit-correlation.md)
   document records.
4. Keep exposing the metrics on the port that the Helm chart records
   (`monitoring.prometheus.port: 9090`).

Descriptor registration and endpoint wiring are complete; business
observations and exemplars remain a future implementation slice.

## SLI categories

The SLO handbook records four SLI categories that cover the Phase 6
acceptance criteria:

- **Availability** — the control plane accepts sign-in requests and
  the API Server returns success responses.
- **Freshness** — the data plane processes records within the
  latency budget.
- **Deliverability** — the Worker writes records to the sink without
  rejection.
- **Audit completeness** — the audit table records every
  authenticated mutation with a `request_id`.

The four categories cover the Phase 6 slices. The handbook defers
the multi-region SLI categories to the Slice 25 follow-up.

## Verification

The slice is verified by:

- Reading the handbook against the existing Prometheus metric
  surface, the existing SLF4J/zap logger conventions, and the
  existing audit-event schema. The handbook must reference every
  metric name, logger name, and audit column that the production
  code already emits.
- Reading the SLO handbook against the Phase 6 acceptance
  document. The acceptance document records the SLIs that the
  Phase 6 slices are responsible for; the SLO handbook must inherit
  those SLIs.
- Running the existing `make check-runbooks` against the handbook
  templates. The templates must satisfy the placeholder and
  hostname requirements.

## Future work

The next observability implementation step is to add business metric
observations and bounded `request_id` exemplars in the Go control plane,
then register and instrument the Java data-plane metric families. Those
steps are out of scope for the current closeout but are explicitly tracked
by the handbook.

The Slice 25 (multi-region) follow-up will inherit the SLO
handbook and add the multi-region SLI categories. The SLO handbook
records the inheritance in the
[`slo-handbook.md`](../../observability/slo-handbook.md) §"SLI
categories" table.
