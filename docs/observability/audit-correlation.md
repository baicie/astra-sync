# Audit Correlation

The audit table records every authenticated mutation (Slice 18,
ADR-042). The Prometheus metrics record the request rate and the
duration. The log records record the lifecycle events. The three
signals share a join key, but the join procedure is not documented.
This document makes the join reproducible.

## Join key

The join key is `request_id`. Audit rows persist it, and F7 attaches it as a
Prometheus exemplar for API Server authentication decisions and authorized
audit queries. Log propagation and exemplar coverage are not yet universal,
so correlation is direct for those F7 metric call sites and for logs that
already carry the field; other investigations use the tenant and timestamp
fallback below.

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

Prometheus exemplars are not enabled merely by using
`prometheus.DefaultRegisterer`. F7 calls `AddWithExemplar` or
`ObserveWithExemplar` for `apiserver_auth_request_total`,
`apiserver_auth_request_duration_seconds`, and
`apiserver_audit_query_duration_seconds`, and enables OpenMetrics content
negotiation on the API Server `/metrics` handler.

The exemplar allowlist contains one label, `request_id`. The recorder accepts
only the canonical lowercase UUID form; arbitrary, uppercase, compact, URN,
or missing values create the normal metric sample without an exemplar. The
ID is never copied into the normal metric label set, so exemplar correlation
does not change time-series cardinality.

## Manual lookup procedure

An operator with no Prometheus exemplar support can join the three
signals manually. The procedure is:

1. Identify `request_id`, `tenant_id`, and `occurred_at` from the audit row.
2. Search the log store for `request_id`. If that field is absent at the
   relevant call site, narrow by `tenant_id`, component, and timestamp.
3. Inspect the metric window for the same tenant/component and timestamp.
   Do not query `request_id` as a metric label; it is intentionally absent
   from the bounded-cardinality metric labels.

For the three F7 metric families, step 3 can provide a direct link back to the
matching request without adding `request_id` as a normal metric label. Other
families continue to use the timestamp fallback.

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

- The deployment Prometheus exemplar retention and Grafana data-source
  settings. F7 records the client-side exemplar contract; the deployment owns
  storage and UI configuration.
- The log store schema. The log store is a deployment-owned
  artefact; the correlation records the join key.
- The audit query interface. The interface is the Slice 18 audit
  explorer's contract; the correlation records the column names.

## Follow-up

F1–F5 deliver logging, descriptor, exposition, and Helm foundations. F7 adds
API Server authentication and audit-query exemplars. Request-context and
exemplar propagation for the remaining control-plane and data-plane call
sites remains incomplete. The landed work is recorded in
[`changelog.md`](changelog.md).

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
