# Metrics Catalog

The catalog is the source of truth for the Prometheus metric names,
labels, and unit conventions that the AstraSync control plane and
data plane emit. The catalog locks the names so the SLO handbook
and the dashboard recipes can reference them without ambiguity.

## Current state

The Helm chart exposes `monitoring.prometheus.enabled: true` and
`monitoring.prometheus.port: 9090`. The `serviceMonitor` resource
default is disabled. The control plane Go modules (`api-server`,
`auth`, `console`) declare the Prometheus client library as a direct
dependency as of Phase 7 Slice 26 follow-up (F4); the `controller`
module uses the metrics endpoint exposed by `controller-runtime`.

The catalog is no longer forward-looking. The metrics the catalog
documents are emitted by the components listed in
[§"Implementation status"](#implementation-status). The follow-up
slices that land the registration and the deployment surface are
recorded in [`changelog.md`](changelog.md).

## Implementation status

The table records the slice that registers each metric and the source
commit on the slice's branch. The `controller_*` metrics are emitted
by `controller-runtime`'s built-in Prometheus collector; the
`apiserver_*`, `scheduler_*`, `connection_test_*`, and `console_*`
metrics are emitted by the dedicated `internal/metrics` packages
introduced in F4. The `coordinator_*` and `worker_*` metrics remain
forward-looking; the Java data plane Micrometer migration is not in
scope for this PR cluster.

| Metric | Component | Slice | Commit |
|---|---|---|---|
| `apiserver_auth_request_total` | api-server | F4 | `2d9debe` |
| `apiserver_auth_request_duration_seconds` | api-server | F4 | `2d9debe` |
| `apiserver_sign_in_total` | api-server | F4 | `2d9debe` |
| `apiserver_session_revoke_total` | api-server | F4 | `2d9debe` |
| `apiserver_audit_query_duration_seconds` | api-server | F4 | `2d9debe` |
| `apiserver_trusted_proxy_hsts_total` | api-server | F4 | `2d9debe` |
| `scheduler_job_assignment_total` | scheduler | F4 | `7e463bb` |
| `scheduler_lease_takeover_total` | scheduler | F4 | `7e463bb` |
| `scheduler_job_reconcile_duration_seconds` | scheduler | F4 | `7e463bb` |
| `connection_test_total` | connection-test-executor | F4 | `abf48c9` |
| `auth_sign_in_total` | auth library | F4 | `5e216db` |
| `auth_session_revoke_total` | auth library | F4 | `5e216db` |
| `console_request_total` | console | F4 | `4336d4e` |
| `console_render_duration_seconds` | console | F4 | `4336d4e` |
| `controller_*` | controller | controller-runtime | upstream collector |

## Naming convention

Every metric name follows the
[Prometheus naming convention](https://prometheus.io/docs/practices/naming/):

- `_total` suffix for counters.
- `_seconds` suffix for time durations.
- `_bytes` suffix for byte counts.
- `_ratio` suffix for ratios in the `[0, 1]` range.
- No `_gauge` or `_counter` suffix. The metric type is inferred from
  the suffix and the registered handler.

Every metric name is prefixed with the component name:

- `apiserver_*` for the API Server.
- `console_*` for the Console BFF.
- `controller_*` for the Controller.
- `scheduler_*` for the Scheduler.
- `coordinator_*` for the Coordinator.
- `worker_*` for the Worker.
- `compiler_*` for the Compiler Validation service.

The prefix matches the Kubernetes deployment name in the Helm chart
(`deployment.helm.astrasync.templates.*.deployment.yaml`). A
Prometheus `job` label that matches the prefix is the operator's
platform convention; the populated dashboard enforces the match.

## Labels

Every metric carries the following labels where applicable:

| Label | Description | Cardinality |
|---|---|---|
| `tenant_id` | Tenant UUID. Dropped on metrics that are not tenant-scoped. | bounded by tenant count |
| `job_id` | Job UUID. Dropped on metrics that are not job-scoped. | bounded by active job count |
| `namespace` | Kubernetes namespace. | constant |
| `component` | Component name (`apiserver`, `controller`, etc.). | constant |
| `outcome` | Outcome label (`success`, `failure`, `rejected`). | constant |

Histogram metrics add a `le` label that Prometheus computes
automatically. The catalog does not enumerate the buckets; the
default Prometheus histogram buckets apply unless the metric
specification overrides them.

## Authentication and authorization metrics

The API Server and the Slice 18 authentication service emit metrics
that join the audit table. The metric names align with the audit
event types.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `apiserver_auth_request_total` | counter | `tenant_id`, `outcome` | Authentication requests received by the API Server. |
| `apiserver_auth_request_duration_seconds` | histogram | `tenant_id`, `outcome` | Time to authenticate a request, including OIDC validation. |
| `apiserver_sign_in_total` | counter | `tenant_id`, `outcome` | Sign-in events, including denied sign-ins. |
| `apiserver_session_revoke_total` | counter | `tenant_id`, `actor_id` | Sessions revoked by the admin CLI or by the audit-driven revocation path. |
| `apiserver_audit_query_duration_seconds` | histogram | `tenant_id` | Time to fulfil a single audit query. |

The histogram metrics emit `le` buckets; the dashboard recipes
compose the P50, P95, and P99 from the buckets.

## Job lifecycle metrics

The Controller and the Scheduler emit metrics that reflect the
phase-4 control-plane lifecycle (ADR-029, ADR-031).

| Metric | Type | Labels | Description |
|---|---|---|---|
| `controller_job_state_total` | counter | `tenant_id`, `namespace`, `from_state`, `to_state` | Job state transitions. |
| `controller_job_controller_reconcile_duration_seconds` | histogram | `tenant_id`, `outcome` | Time to reconcile a single Job against the desired state. |
| `controller_epoch_fence_total` | counter | `tenant_id`, `outcome` | Epoch-fence attempts from the Scheduler. |
| `scheduler_job_assignment_total` | counter | `tenant_id`, `worker_id`, `outcome` | Job assignment attempts from the Scheduler. |
| `scheduler_lease_takeover_total` | counter | `tenant_id`, `outcome` | Leader-lease takeover events. |

The Controller metrics are joined to the audit table by the
`request_id` field recorded in the audit row. The
[`audit-correlation.md`](audit-correlation.md) document records the
join procedure.

## Data plane metrics

The Coordinator and the Worker emit metrics that reflect the
phase-5 Arrow batch and the checkpoint lifecycle (ADR-032, ADR-033).

| Metric | Type | Labels | Description |
|---|---|---|---|
| `coordinator_batch_size_records` | histogram | `tenant_id`, `job_id` | Batch size in records after the adaptive parallelism policy. |
| `coordinator_batch_duration_seconds` | histogram | `tenant_id`, `job_id`, `stage` | Time per stage (read, transform, write) in a single batch. |
| `coordinator_spill_bytes_total` | counter | `tenant_id`, `job_id` | Bytes spilled to object storage when the spillable exchange overflows. |
| `coordinator_checkpoint_duration_seconds` | histogram | `tenant_id`, `job_id` | Time to complete a single checkpoint, including the state-backend write. |
| `worker_records_read_total` | counter | `tenant_id`, `job_id` | Records read by the Worker. |
| `worker_records_written_total` | counter | `tenant_id`, `job_id` | Records written by the Worker. |
| `worker_records_rejected_total` | counter | `tenant_id`, `job_id`, `reason` | Records rejected by the sink writer; the `reason` is a stable code, not a free-form message. |

The `coordinator_*` and `worker_*` metrics are emitted by the Java
data plane. The Java module import of the Prometheus client is
expected to land in the SLF4J migration follow-up slice; the
handbook references the metrics today so the dashboard recipes can
be authored against the eventual shape.

## CLI metrics

The CLI metrics report contract (ADR-020) emits one JSON object per
run on `stdout` or `stderr`. The JSON object is not a Prometheus
metric; it is a per-run summary. The catalog records the report
shape for the operator who consumes the JSON.

```json
{
  "status": "SUCCEEDED",
  "job": "csv-file-copy",
  "deliveryGuarantee": "at-most-once",
  "recordsRead": 2,
  "recordsWritten": 2,
  "batches": 2,
  "maxBatchRecords": 2,
  "elapsedMillis": 17
}
```

The JSON fields are stable. The CLI is the only component that
emits this format; the control plane and the data plane do not
duplicate the shape.

## What the catalog does not record

- The histogram bucket boundaries. The default Prometheus buckets
  apply unless a metric specification overrides them.
- The exact set of `reason` values for `worker_records_rejected_total`.
  The values are stable codes; the catalog defers the enumeration
  to the connector documentation.
- The dashboards that consume the metrics. The
  [`dashboard-recipes.md`](dashboard-recipes.md) document records
  the reference queries.

## Follow-up

The Slice 26 follow-up slices (F1–F5) are the implementation that
backs the catalog. They are recorded in [`changelog.md`](changelog.md)
together with their source commits. The Java data plane Micrometer
migration that emits the `coordinator_*` and `worker_*` metrics is
deferred to a future slice.

## Inline placeholders for the populated handbook

The operator populates the handbook by replacing every `<placeholder>`
value with the environment-specific value. The catalog records the
following inline placeholders that the populated dashboard needs:

- `<cluster-name>` — the Kubernetes cluster where the deployment runs.
- `<prometheus-job-label>` — the Prometheus `job` label that the
  populated dashboard uses to identify the AstraSync scrape target.
- `<alert-thresholds>` — the per-SLO alert thresholds the operator
  configures in the deployment's Prometheus rule file.

The catalog does not record the populated values. The populated
dashboard is a deployment-owned artefact.