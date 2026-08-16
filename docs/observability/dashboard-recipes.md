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

The metric is emitted by the Slice 22 transport hardening
middleware. The recipe is a placeholder for the follow-up slice
that registers the Prometheus client in the API Server.

## Join with the audit table

The recipes that join the metric to the audit table use the
`request_id` field. The join procedure is documented in the
[`audit-correlation.md`](audit-correlation.md) document. The
populated dashboard joins the exemplar to the matching log record
when the operator clicks the `request_id` value in the panel.

The recipes that do not join to the audit table stand alone. The
operator who needs to investigate a regression starts at the
metric, follows the `request_id` to the audit row, and follows the
`request_id` to the log record.

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

The follow-up slice that registers the Prometheus client in the
API Server, the Console, and the auth module must ensure that the
metrics referenced by the recipes are emitted. The follow-up slice
that migrates the Java data plane to SLF4J and registers the
Prometheus client must ensure that the freshness and deliverability
metrics are emitted.

The follow-up slices are recorded in the Phase 7 ADR-047
Consequences section so the boundary between the documentation-only
Slice 26 and the code-migration follow-ups stays clear.

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