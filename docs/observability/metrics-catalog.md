# Metrics Catalog

The catalog is the source of truth for the Prometheus metric names,
labels, and unit conventions that the AstraSync control plane and
data plane emit. The catalog locks the names so the SLO handbook
and the dashboard recipes can reference them without ambiguity.

## Current state

The Helm chart exposes `monitoring.prometheus.enabled: true` and
`monitoring.prometheus.port: 9090`; `serviceMonitor` remains disabled by
default. The API Server, Scheduler, Connection Test Executor, and Console
declare the Prometheus client directly, register metric descriptors, and
bind a dedicated `/metrics` listener when `METRICS_LISTEN_ADDRESS` is set.
An empty address disables that listener. The auth module contains a
descriptor package, but its one-shot admin CLI neither imports it nor binds
an endpoint. The Controller continues to use controller-runtime's endpoint.

F7 adds business instrumentation for API Server authentication decisions and
authorized audit queries. Those call sites update three families and attach a
canonical UUID `request_id` through `AddWithExemplar` or
`ObserveWithExemplar`. The API Server handler enables OpenMetrics content
negotiation, which is required to transmit those exemplars. The remaining
descriptors are still registration-only; a scrape exposes their business
series only after a future call site creates a labelled sample.

## Implementation status

The table separates descriptor availability from sampled runtime data.

| Metric family | Component | Registration/exposition | Business samples |
|---|---|---|---|
| `apiserver_auth_request_total`, `apiserver_auth_request_duration_seconds` | api-server | F4 descriptor + `/metrics` | emitted by F7 authentication interceptor |
| `apiserver_audit_query_duration_seconds` | api-server | F4 descriptor + `/metrics` | emitted by F7 authorized audit-query path |
| remaining `apiserver_*` listed below | api-server | F4 descriptor + `/metrics` | pending |
| `scheduler_*` listed below | scheduler | F4 descriptor + `/metrics` | pending |
| `connection_test_total` | connection-test-executor | F4 descriptor + `/metrics` | pending |
| `console_*` listed below | console | F4 descriptor + `/metrics` | pending |
| `auth_*` listed below | auth library | descriptor package only | pending |
| controller-runtime built-ins | controller | upstream endpoint | emitted by controller-runtime |
| `coordinator_*`, `worker_*` listed below | Java data plane | not registered | pending |

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
| `tenant_id` | Trusted tenant UUID. Authentication decisions before tenant resolution use `_unknown`; self-scope methods use `_platform`. | bounded by tenant count plus two fixed values |
| `job_id` | Job UUID. Dropped on metrics that are not job-scoped. | bounded by active job count |
| `namespace` | Kubernetes namespace. | constant |
| `component` | Component name (`apiserver`, `controller`, etc.). | constant |
| `outcome` | Outcome label (`success`, `failure`, `rejected`). | constant |
| `actor_id` | Principal UUID for actor-scoped operations. | bounded by active principals |
| `worker_id` | Worker identifier for assignment operations. | bounded by worker count |
| `handler` | Stable Console handler name. | constant allowlist |

Histogram metrics add a `le` label that Prometheus computes
automatically. The catalog does not enumerate the buckets; the
default Prometheus histogram buckets apply unless the metric
specification overrides them.

## Authentication and authorization metrics

The API Server and auth descriptor packages define metrics that align with
the audit event types. F7 activates the authentication decision counter and
histogram plus the authorized audit-query histogram. The other rows remain
descriptor-only.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `apiserver_auth_request_total` | counter | `tenant_id`, `outcome` | Completed API Server authentication and authorization decisions. |
| `apiserver_auth_request_duration_seconds` | histogram | `tenant_id`, `outcome` | Decision time through authorization, excluding business-handler execution. |
| `apiserver_sign_in_total` | counter | `tenant_id`, `outcome` | Sign-in events, including denied sign-ins. |
| `apiserver_session_revoke_total` | counter | `tenant_id`, `actor_id` | Sessions revoked by the admin CLI or by the audit-driven revocation path. |
| `apiserver_audit_query_duration_seconds` | histogram | `tenant_id` | Time to fulfil one authorized audit query, including failures after authorization. |
| `apiserver_trusted_proxy_hsts_total` | counter | `tenant_id` | HSTS responses reserved for trusted-proxy middleware instrumentation. |
| `auth_sign_in_total` | counter | `tenant_id`, `outcome` | Auth-library sign-in descriptor; the admin CLI does not expose it. |
| `auth_session_revoke_total` | counter | `tenant_id` | Auth-library revoke descriptor; the admin CLI does not expose it. |

The authentication `outcome` allowlist is:

- `success`: authentication and authorization completed and the request was
  admitted to its business handler.
- `rejected`: invalid credentials, invalid request scope, or insufficient
  permission. Caller-controlled rejection traffic does not consume the
  service availability error budget.
- `failure`: an authentication dependency, policy state, or authenticated
  principal invariant failed internally.

Before authentication resolves a membership, the recorder uses the fixed
`_unknown` tenant value rather than caller input. Self-scope methods use the
fixed `_platform` value. Once a membership is resolved, only its validated
tenant UUID is used. Audit-query observations begin only after authorization
returns a trusted tenant decision, so unauthorized requests cannot create
tenant series.

F7 attaches `request_id` exemplars to the authentication counter and
histogram and to the audit-query histogram only when the value is a canonical
lowercase UUID. Other values still produce the bounded metric sample but no
exemplar. `request_id` is never a normal time-series label. Histogram metrics
emit `le` buckets; the dashboard recipes compose P50, P95, and P99 from them.

## Job lifecycle metrics

The table reserves lifecycle metrics for the Controller and Scheduler
(ADR-029, ADR-031). The F4 Scheduler descriptors exist but are not observed;
the named custom Controller metrics are not implemented by
controller-runtime's generic collector.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `controller_job_state_total` | counter | `tenant_id`, `namespace`, `from_state`, `to_state` | Job state transitions. |
| `controller_job_controller_reconcile_duration_seconds` | histogram | `tenant_id`, `outcome` | Time to reconcile a single Job against the desired state. |
| `controller_epoch_fence_total` | counter | `tenant_id`, `outcome` | Epoch-fence attempts from the Scheduler. |
| `scheduler_job_assignment_total` | counter | `tenant_id`, `worker_id`, `outcome` | Job assignment attempts from the Scheduler. |
| `scheduler_lease_takeover_total` | counter | `tenant_id`, `outcome` | Leader-lease takeover events. |
| `scheduler_job_reconcile_duration_seconds` | histogram | `tenant_id` | Time to reconcile one scheduled Job. |

Future call-site instrumentation can correlate these metrics to audit rows
through exemplars; that wiring is not present in the current implementation.

## Connection test and Console metrics

F4 registers the following descriptors and exposes them from the owning
long-running executable. Their business call sites remain unwired.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `connection_test_total` | counter | `tenant_id`, `outcome` | Connection-test outcomes. |
| `console_request_total` | counter | `tenant_id`, `outcome`, `handler` | Console request outcomes by stable handler name. |
| `console_render_duration_seconds` | histogram | `handler` | Console rendering duration. |

## Data plane metrics

The following names are reserved for future Coordinator and Worker
instrumentation of the Phase 5 Arrow batch and checkpoint lifecycle
(ADR-032, ADR-033). They are not registered or emitted today.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `coordinator_batch_size_records` | histogram | `tenant_id`, `job_id` | Batch size in records after the adaptive parallelism policy. |
| `coordinator_batch_duration_seconds` | histogram | `tenant_id`, `job_id`, `stage` | Time per stage (read, transform, write) in a single batch. |
| `coordinator_spill_bytes_total` | counter | `tenant_id`, `job_id` | Bytes spilled to object storage when the spillable exchange overflows. |
| `coordinator_checkpoint_duration_seconds` | histogram | `tenant_id`, `job_id` | Time to complete a single checkpoint, including the state-backend write. |
| `worker_records_read_total` | counter | `tenant_id`, `job_id` | Records read by the Worker. |
| `worker_records_written_total` | counter | `tenant_id`, `job_id` | Records written by the Worker. |
| `worker_records_rejected_total` | counter | `tenant_id`, `job_id`, `reason` | Records rejected by the sink writer; the `reason` is a stable code, not a free-form message. |

The Java data plane has no Prometheus/Micrometer client wiring. These names
remain a dashboard contract for a future implementation slice.

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

F4 and F5 provide descriptor packages, HTTP exposition, and Helm discovery;
F7 activates the three API Server SLO families documented above. Business
observations for the remaining API Server, Console, Scheduler, Connection
Test Executor, and auth-library descriptors, plus Java data-plane metrics,
remain deferred. The landed work is recorded in
[`changelog.md`](changelog.md).

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
