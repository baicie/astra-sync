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
`METRICS_LISTEN_ADDRESS` is non-empty. F7 implements the first business
instrumentation step:

1. Inject one recorder into the authentication interceptor and AuditService
   rather than mutating global vectors from business logic.
2. Record `success`, `rejected`, and `failure` authentication decisions once
   before the business handler. The availability denominator is
   `success|failure`; caller-controlled `rejected` traffic is excluded.
3. Use only validated membership tenant UUIDs. Pre-tenant failures use the
   fixed `_unknown` value, and self-scope methods use `_platform`.
4. Observe audit-query duration only after authorization returns a trusted
   tenant decision, and reuse the same request ID for the audit row and metric
   exemplar.
5. Attach a single `request_id` exemplar only for canonical lowercase UUIDs;
   never add it to the normal metric label set.
6. Enable OpenMetrics negotiation on the existing endpoint and keep exposing
   metrics on the port that the Helm chart records
   (`monitoring.prometheus.port: 9090`).

Descriptor registration and endpoint wiring are complete. F7 activates
`apiserver_auth_request_total`,
`apiserver_auth_request_duration_seconds`, and
`apiserver_audit_query_duration_seconds`; other business observations remain
future slices.

## SLI categories

The SLO handbook records four SLI categories that cover the Phase 6
acceptance criteria:

- **Availability** — the API Server completes authentication and
  authorization without an internal failure.
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
- Running API Server metric, authentication interceptor, AuditService, and
  server tests twice to verify deterministic observations and OpenMetrics
  exemplar exposition.

## Future work

The next observability implementation step is to activate the remaining Go
control-plane descriptors, then register and instrument the Java data-plane
metric families. F7 completes the API Server authentication and audit-query
subset with bounded `request_id` exemplars.

The Slice 25 (multi-region) follow-up will inherit the SLO
handbook and add the multi-region SLI categories. The SLO handbook
records the inheritance in the
[`slo-handbook.md`](../../observability/slo-handbook.md) §"SLI
categories" table.
