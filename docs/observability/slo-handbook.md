# SLO Handbook

The SLO handbook records the per-tenant SLI and SLO definitions that
the operator consumes through the populated dashboard. The handbook
is a template; the populated SLO targets are deployment-owned.

F4/F5 provide Go metric descriptors and scrape endpoints. F7 makes the API
Server availability and audit-query latency expressions live by instrumenting
authentication decisions and authorized audit queries with bounded
`request_id` exemplars. Freshness and deliverability remain target contracts
because the Java data-plane metric families are not registered. The SQL
audit-completeness query remains independently usable.

## SLI categories

The handbook records four SLI categories. Each category is a
single dimension that the operator can monitor from the populated
dashboard.

| Category | Description | Source metric |
|---|---|---|
| Availability | The API Server completes authentication and authorization without an internal failure. | `apiserver_auth_request_total` |
| Freshness | The data plane processes records within the latency budget. | `coordinator_batch_duration_seconds` |
| Deliverability | The Worker writes records to the sink without rejection. | `worker_records_rejected_total` |
| Audit completeness | The audit table records every authenticated mutation. | `apiserver_audit_query_duration_seconds` |

The four categories cover the Phase 6 acceptance criteria. The
handbook defers the multi-region SLI categories to the Slice 25
follow-up.

## Availability

### SLI

The availability SLI is the ratio of successful authentication decisions to
all service-accountable authentication decisions over a rolling window.

```promql
sum(rate(apiserver_auth_request_total{outcome="success"}[5m]))
/
sum(rate(apiserver_auth_request_total{outcome=~"success|failure"}[5m]))
```

`rejected` is excluded from the denominator because invalid credentials,
invalid scope, and insufficient permission are caller decisions rather than
service downtime. Internal authentication, policy, and principal-state
errors use `failure` and consume the error budget. Failures before trusted
tenant resolution use `_unknown`; self-scope decisions use `_platform`.
The dashboard records the SLI as a percentage. The target is the
deployment-side SLO target.

### SLO target

The SLO target is the deployment's contractual availability target.
A typical target is `99.9%` over a rolling 30-day window. The
target is a deployment-owned value; the handbook records the
metric and the selector.

### Error budget

The error budget is the inverse of the SLO target. A 99.9% SLO
target over a rolling 30-day window allows 43.2 minutes of downtime
per month. The dashboard recipes in the
[`dashboard-recipes.md`](dashboard-recipes.md) document record the
error budget query.

## Freshness

### SLI

The freshness SLI is the ratio of completed batches whose duration
falls within the latency budget to total completed batches.

```promql
sum(rate(coordinator_batch_duration_seconds_bucket{le="<latency-budget>"}[5m]))
/
sum(rate(coordinator_batch_duration_seconds_count[5m]))
```

The `<latency-budget>` is the deployment-side latency budget. The
handbook records the metric and the selector.

### SLO target

The SLO target is the deployment's contractual freshness target. A
typical target is the 95th percentile of the batch duration. The
target is a deployment-owned value.

## Deliverability

### SLI

The deliverability SLI is the ratio of successful records written
to the sink to total records read from the source.

```promql
1 - (
  sum(rate(worker_records_rejected_total[5m]))
  /
  sum(rate(worker_records_read_total[5m]))
)
```

The deliverability SLI is the inverse of the rejection rate. The
handbook records the metric and the selector.

### SLO target

The SLO target is the deployment's contractual deliverability
target. A typical target is `99.99%` over a rolling 30-day window.
The target is a deployment-owned value.

## Audit completeness

### SLI

The audit completeness SLI is the ratio of audit rows that have a
non-empty `request_id` to total audit rows.

The SLI is computed at the audit table level, not at the Prometheus
level. The handbook records the metric and the SQL query:

```sql
SELECT
  count(*) FILTER (WHERE request_id IS NOT NULL AND request_id <> '') AS complete,
  count(*) AS total
FROM audit_events
WHERE occurred_at >= now() - interval '30 days';
```

The SLI is a percentage. The target is the deployment-side SLO
target.

### SLO target

The SLO target is `100%`. A row that lacks a `request_id` is a
regression in the audit instrumentation. The dashboard recipes
record the diff query:

```sql
SELECT
  event_id, event_type, actor_id, occurred_at
FROM audit_events
WHERE request_id IS NULL OR request_id = ''
ORDER BY occurred_at DESC
LIMIT 100;
```

The diff query is the operator's entry point for an audit
completeness regression.

## Per-slice SLIs

The Phase 6 acceptance document records the SLIs that the Phase 6
slices are responsible for. The handbook inherits the SLIs from
the acceptance document. The cross-reference is:

| Slice | SLI category | Metric |
|---|---|---|
| Slice 18 (auth, RBAC, audit) | Availability, audit completeness | `apiserver_auth_request_total`, `apiserver_audit_query_duration_seconds` |
| Slice 20 (connector catalog) | Deliverability | `worker_records_rejected_total` |
| Slice 21 (audit explorer) | Audit completeness | `apiserver_audit_query_duration_seconds` |
| Slice 22 (transport hardening) | Availability | `apiserver_auth_request_total` |
| Slice 23 (control-plane mTLS) | Availability | `apiserver_auth_request_total` |

The cross-reference is the source of truth for the SLI mapping. The
Phase 7 acceptance document will record the Phase 7 SLIs when the
follow-up migration slice lands.

## What the handbook does not record

- The SLO targets. The targets are deployment-owned values; the
  handbook records the metrics and the selectors.
- The error budget policies. The policies are the deployment's
  customer-facing commitments; the handbook records the
  calculation.
- The alert thresholds. The thresholds are the deployment's
  on-call policies; the [`dashboard-recipes.md`](dashboard-recipes.md)
  document records the reference queries.

## Follow-up

F7 completes API Server authentication and audit-query observations with
bounded `request_id` exemplars. Remaining follow-up work must instrument the
other Go control-plane descriptors and register the Java data-plane families
used by freshness and deliverability. The completed slices are recorded in
ADR-047 and the observability changelog.

<!-- placeholders: slo-availability-target, slo-freshness-budget, slo-deliverability-target, audit-retention-days -->
