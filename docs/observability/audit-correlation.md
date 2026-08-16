# Audit Correlation

The audit table records every authenticated mutation (Slice 18,
ADR-042). The Prometheus metrics record the request rate and the
duration. The log records record the lifecycle events. The three
signals share a join key, but the join procedure is not documented.
This document makes the join reproducible.

## Join key

The join key is the `request_id` field. The audit row, the log
record, and the Prometheus exemplar all carry the `request_id`. The
field is a UUID assigned by the API Server for HTTP requests and
by the data plane for batch-processing events.

The `request_id` is propagated through the gRPC interceptor (API
Server and Console) and the Coordinator's checkpoint lifecycle.
The `request_id` is recorded in the audit table's `request_id`
column and as the SLF4J structured field recorded in the
[`log-conventions.md`](log-conventions.md) §"Structured fields"
table.

## Audit table columns

The audit table is `audit_events` in the Slice 18 schema. The
columns that the join procedure consumes are:

| Column | Type | Description |
|---|---|---|
| `event_id` | UUID | Audit event UUID. |
| `event_type` | string | Audit event type (`create-job`, `sign-in`, etc.). |
| `actor_id` | UUID | Principal UUID. |
| `tenant_id` | UUID | Tenant UUID. |
| `request_id` | UUID | The join key. |
| `outcome` | string | Outcome label (`SUCCESS`, `FAILURE`, `DENIED`). |
| `attributes` | JSONB | Event-type-specific attributes. |
| `occurred_at` | timestamptz | Event timestamp. |

The audit table is partitioned by `occurred_at` month. The join
procedure must respect the partition window; the audit retention
runbook (ADR-046) records the partition rollover.

## Prometheus exemplar

The Prometheus metrics documented in the
[`metrics-catalog.md`](metrics-catalog.md) record the `request_id`
as a Prometheus exemplar. The exemplar is enabled by the
`prometheus.EnableExemplars` option in the Prometheus client
configuration. The follow-up slice that registers the Prometheus
client in the API Server, the Console, and the auth module records
the option.

The exemplar is keyed by the metric name and the label set. The
populated dashboard joins the exemplar to the matching log record
by the `request_id` field. The dashboard recipes in the
[`dashboard-recipes.md`](dashboard-recipes.md) document reference
the exemplar by the metric name.

## Manual lookup procedure

An operator with no Prometheus exemplar support can join the three
signals manually. The procedure is:

1. Identify the `request_id` from the audit row. The audit row's
   `request_id` is the canonical join key.
2. Search the log store for the `request_id` field. The log
   records recorded by the API Server, the Console, and the data
   plane share the `request_id`.
3. Search the Prometheus metrics for the `request_id` label. The
   metrics emitted by the API Server, the Console, and the data
   plane share the `request_id` label.

The procedure is the manual fallback when the Prometheus exemplar
is not available. The procedure is also the integration test the
follow-up slice that registers the Prometheus client must satisfy.

## Per-tenant join

The audit table is tenant-scoped. The Prometheus metrics that carry
the `tenant_id` label are joined tenant-by-tenant. The log records
that carry the `tenant_id` field are joined tenant-by-tenant. The
join procedure respects the tenant authorization boundary recorded
by ADR-042.

The SLO handbook in the [`slo-handbook.md`](slo-handbook.md)
document records the per-tenant SLI/SLO definitions. The
[`dashboard-recipes.md`](dashboard-recipes.md) document records the
per-tenant dashboard queries.

## Slice 18 admin CLI

The Slice 18 admin CLI (`astra-auth-admin`) is the operator's entry
point for the audit table. The `show-tenant` and `show-revision`
commands print the audit event rows. The CLI does not connect to
the Prometheus metrics or the log records; the operator uses the
CLI to find the `request_id` and then uses the log store and the
Prometheus query interface to follow the join.

The
[`docs/runbooks/session-revocation.md`](../runbooks/session-revocation.md)
template documents the procedure for revoking a session; the
audit-correlation procedure is the reverse direction: an operator
investigates a session by joining the audit row to the log records
and the metrics.

## What the correlation does not record

- The Prometheus exemplar configuration. The exemplar is a
  Prometheus client option; the follow-up slice records the option.
- The log store schema. The log store is a deployment-owned
  artefact; the correlation records the join key.
- The audit query interface. The interface is the Slice 18 audit
  explorer's contract; the correlation records the column names.

## Follow-up

The follow-up slice that migrates the Java data plane to SLF4J
must record the `request_id` field in every structured log record
that the Coordinator and the Worker emit. The follow-up slice that
migrates the API Server, the Console, and the auth module to zap
must record the same field.

The follow-up slice that registers the Prometheus client in the
API Server, the Console, and the auth module must register the
exemplar handler. The follow-up slice that registers the Prometheus
client in the Java module must register the same handler.

The follow-up slices are recorded in the Phase 7 ADR-047
Consequences section so the boundary between the documentation-only
Slice 26 and the code-migration follow-ups stays clear.

## Inline placeholders for the populated handbook

The operator populates the correlation by replacing every `<placeholder>`
value with the environment-specific value. The correlation records the
following inline placeholders that the populated deployment needs:

- `<log-store>` — the deployment log store URL. The operator uses this
  URL to follow the `request_id` join key to the log record.
- `<prometheus-endpoint>` — the Prometheus endpoint URL. The operator
  uses this endpoint to query the metric exemplars.
- `<audit-query-tool>` — the audit query tool the operator uses to
  find the `request_id`. The default is the Slice 18 admin CLI
  (`astra-auth-admin show-tenant`).

The correlation does not record the populated values. The populated
deployment is a deployment-owned artefact.