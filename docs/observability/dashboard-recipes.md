# Dashboard Recipes

The dashboard recipes are the reference queries that the populated
Grafana dashboard consumes. The recipes are templates; the
populated dashboard is a deployment-owned artefact.

## Convention

Each recipe is a panel that records the SLI category, the metric
name, the suggested visualisation, and the source contract. The
recipe does not record the threshold; the threshold is the
deployment's SLO target.

The recipes use Prometheus as the query language. The companion
queries for the audit table are SQL recipes that the operator runs
against the PostgreSQL audit database.

## Implementation status

These PromQL expressions are target contracts. F4/F5 register the Go
descriptors, expose `/metrics`, and wire Prometheus discovery, but production
business call sites do not yet create the referenced samples. The
`coordinator_*` and `worker_*` families are not registered at all. Operators
must not use these recipes as live SLO evidence until the corresponding
call-site instrumentation has landed.

## Availability recipes

### Sign-in success rate (per tenant)

```promql
sum by (tenant_id) (
  rate(apiserver_auth_request_total{outcome="success"}[5m])
)
/
sum by (tenant_id) (
  rate(apiserver_auth_request_total[5m])
)
```

Suggested visualisation: `stat` panel with the threshold colour
encoded in the dashboard. The threshold is the deployment-side SLO
target.

### Sign-in latency (per tenant)

```promql
histogram_quantile(
  0.95,
  sum by (tenant_id, le) (
    rate(apiserver_auth_request_duration_seconds[5m])
  )
)
```

Suggested visualisation: `timeseries` panel with the P50, P95, and
P99 lines.

## Freshness recipes

### Batch duration (per job)

```promql
histogram_quantile(
  0.95,
  sum by (job_id, stage, le) (
    rate(coordinator_batch_duration_seconds[5m])
  )
)
```

Suggested visualisation: `timeseries` panel with the stage label as
the legend.

### Spillable exchange overflow (per job)

```promql
sum by (job_id) (
  rate(coordinator_spill_bytes_total[5m])
)
```

Suggested visualisation: `timeseries` panel with the spill byte rate.

### Checkpoint duration (per job)

```promql
histogram_quantile(
  0.95,
  sum by (job_id, le) (
    rate(coordinator_checkpoint_duration_seconds[5m])
  )
)
```

Suggested visualisation: `timeseries` panel with the P50, P95, and
P99 lines.

## Deliverability recipes

### Record rejection rate (per job)

```promql
sum by (job_id, reason) (
  rate(worker_records_rejected_total[5m])
)
/
sum by (job_id) (
  rate(worker_records_read_total[5m])
)
```

Suggested visualisation: `timeseries` panel with the `reason` label
as a separate series.

### Throughput (per job)

```promql
sum by (job_id) (
  rate(worker_records_written_total[5m])
)
```

Suggested visualisation: `timeseries` panel with the throughput line.

## Audit completeness recipes

### Audit requests with missing request_id

```sql
SELECT
  occurred_at,
  event_type,
  actor_id,
  tenant_id
FROM audit_events
WHERE request_id IS NULL OR request_id = ''
ORDER BY occurred_at DESC
LIMIT 100;
```

Suggested visualisation: `table` panel with a row per missing
audit row.

### Audit query duration (per tenant)

```promql
histogram_quantile(
  0.95,
  sum by (tenant_id, le) (
    rate(apiserver_audit_query_duration_seconds[5m])
  )
)
```

Suggested visualisation: `timeseries` panel with the P50, P95, and
P99 lines.

## Slice 22 transport hardening

The API Server and the Console emit the security headers recorded
by ADR-043. The dashboard recipes record the rate of the security
header responses:

### Trusted-proxy trusted-peer rate

```promql
sum by (tenant_id) (
  rate(apiserver_trusted_proxy_hsts_total[5m])
)
```

F4 registers this descriptor, but the Slice 22 middleware does not yet
increment it. The recipe remains inactive until that call site is wired.

## Join with the audit table

The target join uses a `request_id` exemplar as documented in
[`audit-correlation.md`](audit-correlation.md). Exemplars are not currently
emitted, so the populated dashboard cannot yet offer a direct request link.
Until that follow-up lands, operators correlate by tenant, component, and
timestamp and use `request_id` only where the log call site already supplies
it.

## What the recipes do not record

- The alert thresholds. The thresholds are the deployment's
  on-call policies; the recipes record the SLI category and the
  query.
- The Grafana manifest. The manifest is a deployment-owned
  artefact; the recipes record the queries and the
  visualisation suggestions.
- The SLO targets. The targets are the deployment's contractual
  values; the recipes record the metric and the selector.

## Follow-up

The remaining implementation must instrument the API Server, Console,
Scheduler, Connection Test Executor, and server-side auth call sites; add
bounded exemplars; and register the Java data-plane metric families. This is
separate from the completed descriptor and endpoint foundation recorded in
ADR-047.

## Inline placeholders for the populated handbook

The operator populates the recipes by replacing every `<placeholder>`
value with the environment-specific value. The recipes record the
following inline placeholders that the populated dashboard needs:

- `<deployment-cluster>` — the Kubernetes cluster where the
  deployment runs. The populated dashboard uses this value to label
  the panels.
- `<grafana-folder>` — the Grafana folder where the populated
  dashboard lives. The default is `AstraSync`.
- `<pagerduty-service-key>` — the PagerDuty service key that the
  alert rules use to page the on-call operator.

The recipes do not record the populated values. The populated
dashboard is a deployment-owned artefact.
