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
negotiation, which is required to transmit those exemplars. F10 activates
`connection_test_total` after the executor durably completes a claimed test;
F11 activates `console_request_total` and
`console_render_duration_seconds`. F12 activates
`apiserver_trusted_proxy_hsts_total` when the API Server emits HSTS for a
trusted proxy request; sign-in and session-revoke descriptors remain
registration-only because those flows are owned by the Console/auth boundary.
F13 activates the Controller reconcile-duration sample with fixed `_unknown`
tenant scope and a bounded outcome label.
Phase 10 verifies that the API Server exposes the multi-region promotion,
event, and recovery samples from one shared registry.

## Implementation status

The table separates descriptor availability from sampled runtime data.

| Metric family | Component | Registration/exposition | Business samples |
|---|---|---|---|
| `apiserver_auth_request_total`, `apiserver_auth_request_duration_seconds` | api-server | F4 descriptor + `/metrics` | emitted by F7 authentication interceptor |
| `apiserver_audit_query_duration_seconds` | api-server | F4 descriptor + `/metrics` | emitted by F7 authorized audit-query path |
| `apiserver_sign_in_total`, `apiserver_session_revoke_total` | api-server | F4 descriptor + `/metrics` | pending; no API Server business call site owns these Console/auth flows |
| `apiserver_trusted_proxy_hsts_total` | api-server | F4 descriptor + `/metrics` | emitted by F12 trusted-proxy HSTS middleware |
| `scheduler_*` listed below | scheduler | F4 descriptor + `/metrics` | assignment, lease-takeover, and reconcile-duration samples emitted by the Scheduler |
| `astrasync_multi_region_promotion_*`, `astrasync_multi_region_event_*`, `astrasync_multi_region_recovery_*` | control-plane replication | recorder registration; API Server exposition is embedding-owned | promotion, event-delivery, and recovery samples emitted when a recorder is injected; Phase 10 verifies the API Server scrape path |
| `connection_test_total` | connection-test-executor | F4 descriptor + `/metrics` | emitted by F10 after durable test completion |
| `console_*` listed below | console | F4 descriptor + `/metrics` | Console BFF request and render samples emitted by F11 |
| `auth_*` listed below | auth library | descriptor package only | pending |
| `controller_job_controller_reconcile_duration_seconds` | controller | controller-runtime `/metrics` | emitted by F13 with a fixed `_unknown` tenant scope |
| remaining custom Controller metrics | controller | controller-runtime `/metrics` | pending; no stable Controller call site owns them |
| `coordinator_*`, `worker_*` listed below | Java data plane | F8 Micrometer registry + opt-in Worker `/metrics` | seven families emitted by checkpoint Coordinator and in-process Worker; spill bytes are sampled by the Worker-local exchange |

## Naming convention

Every metric name follows the
[Prometheus naming convention](https://prometheus.io/docs/practices/naming/):

- `_total` suffix for counters.
- `_seconds` suffix for time durations.
- `_bytes` suffix for byte counts.
- `_ratio` suffix for ratios in the `[0, 1]` range.
- No `_gauge` or `_counter` suffix. The metric type is inferred from
  the suffix and the registered handler.

Multi-region replication metrics use the `astrasync_multi_region_` prefix because they are
owned by the shared control-plane replication package rather than one executable.

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
histogram plus the authorized audit-query histogram. F12 activates trusted
proxy HSTS observations. Sign-in and session-revoke rows remain
descriptor-only because their business paths are owned by the Console/auth
boundary.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `apiserver_auth_request_total` | counter | `tenant_id`, `outcome` | Completed API Server authentication and authorization decisions. |
| `apiserver_auth_request_duration_seconds` | histogram | `tenant_id`, `outcome` | Decision time through authorization, excluding business-handler execution. |
| `apiserver_sign_in_total` | counter | `tenant_id`, `outcome` | Sign-in events, including denied sign-ins. |
| `apiserver_session_revoke_total` | counter | `tenant_id`, `actor_id` | Sessions revoked by the admin CLI or by the audit-driven revocation path. |
| `apiserver_audit_query_duration_seconds` | histogram | `tenant_id` | Time to fulfil one authorized audit query, including failures after authorization. |
| `apiserver_trusted_proxy_hsts_total` | counter | `tenant_id` | HSTS responses emitted for HTTPS requests accepted from a trusted proxy; F12 records the pre-auth `_unknown` tenant value. |
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
(ADR-029, ADR-031). Scheduler emits assignment and lease-takeover samples;
F13 adds the Controller reconcile-duration sample; the state-transition and
epoch-fence families remain pending because their durable transition owners
are outside the Controller reconcile boundary.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `controller_job_state_total` | counter | `tenant_id`, `namespace`, `from_state`, `to_state` | Job state transitions. |
| `controller_job_controller_reconcile_duration_seconds` | histogram | `tenant_id`, `outcome` | Time to reconcile a single `SyncJob`; F13 uses `_unknown` before a trusted tenant binding exists and records `success` or `failure`. |
| `controller_epoch_fence_total` | counter | `tenant_id`, `outcome` | Epoch-fence attempts from the Scheduler. |
| `scheduler_job_assignment_total` | counter | `tenant_id`, `worker_id`, `outcome` | Assignment outcome when the Scheduler first dispatches a claimed execution. The current dispatch contract has no trusted tenant or target worker identity, so both labels are `_unknown`; `outcome` is `success`, `rejected`, or `failure`. |
| `scheduler_lease_takeover_total` | counter | `tenant_id`, `outcome` | Successful dispatch-lease takeovers returned by the durable claim transaction. `tenant_id` is `_unknown` until the Scheduler receives a trusted tenant binding; `outcome` is `success`. |
| `scheduler_job_reconcile_duration_seconds` | histogram | `tenant_id` | Time to reconcile one scheduled Job. |

Future call-site instrumentation can correlate these metrics to audit rows
through exemplars; that wiring is not present in the current implementation.

## Multi-region promotion metrics

| Metric | Type | Labels | Description |
|---|---|---|---|
| `astrasync_multi_region_promotion_total` | counter | `target_region`, `outcome` | Promotion attempts after a promotion record is created; `outcome` is `success` or `failure`. |
| `astrasync_multi_region_promotion_duration_seconds` | histogram | `target_region` | Duration of promotion attempts after a promotion record is created. |
| `astrasync_multi_region_event_total` | counter | `peer_region`, `event_type`, `outcome` | Cross-region event delivery attempts. `event_type` is `checkpoint`, `topology`, or `health`; `outcome` is `success` or `failure`. |
| `astrasync_multi_region_event_duration_seconds` | histogram | `peer_region`, `event_type` | Duration of cross-region event delivery attempts. |
| `astrasync_multi_region_recovery_total` | counter | `target_region`, `outcome` | Checkpoint recovery attempts. `outcome` is `success` or `failure`; an unset target region is `_unknown`. |
| `astrasync_multi_region_recovery_duration_seconds` | histogram | `target_region` | Duration of checkpoint recovery attempts. |
The recorder uses an injected Prometheus registerer so embedding services can expose the
families from their own endpoint without creating a second listener or global registration.
The API Server injects the recorder into the replication service and runtime,
then combines that registry with its default registry on the optional
`/metrics` listener.

## Connection test and Console metrics

F4 registers the descriptors and exposes them from the owning long-running
executables. F10 wires the Connection Test Executor completion path and F11
wires the Console BFF request path.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `connection_test_total` | counter | `tenant_id`, `outcome` | Completed Connection Test Executor operations. `outcome` is `success`, `rejected` for an egress-policy denial, or `failure` for timeout, cancellation, credential, transport, or handshake failure. The sample is recorded only after the durable completion succeeds. |
| `console_request_total` | counter | `tenant_id`, `outcome`, `handler` | Console request outcomes by stable handler name. F11 records `success` for 2xx/3xx responses, `rejected` for 4xx responses, and `failure` for 5xx responses. The tenant comes from the server-written scope response header and falls back to `_unknown`. |
| `console_render_duration_seconds` | histogram | `handler` | HTML response duration for the Console's fixed `static` handler. |

## Data plane metrics

The Java data plane activates all seven reserved families through Micrometer.
`CheckpointBatchCoordinator` emits the three `coordinator_*` batch/checkpoint
families, while `InProcessBatchWorker` emits the three `worker_*` record
families. `coordinator_spill_bytes_total` retains its Coordinator semantic name
but is sampled by the Worker-local spillable exchange after a payload is
successfully written and enqueued. The Worker exposes the shared process
registry at `/metrics` only when `METRICS_LISTEN_ADDRESS` is non-empty;
Coordinator remains a one-shot process and does not bind a listener. The
non-checkpoint spill path has no trusted job or tenant identity, so its labels
are `tenant_id="_unknown"` and `job_id="_unknown"`.

| Metric | Type | Labels | Description |
|---|---|---|---|
| `coordinator_batch_size_records` | histogram | `tenant_id`, `job_id` | Batch size in records after the adaptive parallelism policy. |
| `coordinator_batch_duration_seconds` | histogram | `tenant_id`, `job_id`, `stage` | Time per stage (read, transform, write) in a single batch. |
| `coordinator_spill_bytes_total` | counter | `tenant_id`, `job_id` | Encoded payload bytes durably written to the Worker-local spill directory and successfully enqueued when the spillable exchange overflows; failed writes, filesystem metadata, consumption, and cleanup are excluded. |
| `coordinator_checkpoint_duration_seconds` | histogram | `tenant_id`, `job_id`, `outcome` | Time to complete a single checkpoint, including the state-backend write. `outcome` is `success` or `failure`. |
| `worker_records_read_total` | counter | `tenant_id`, `job_id` | Records read by the Worker. |
| `worker_records_written_total` | counter | `tenant_id`, `job_id` | Records written by the Worker. |
| `worker_records_rejected_total` | counter | `tenant_id`, `job_id`, `reason` | Records rejected by the sink writer; the `reason` is a stable code, not a free-form message. |

The Java data plane exposes these families through Micrometer; the legacy
sentence below is superseded by the F8 activation described above.

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
F7 activates the three API Server SLO families, F8 activates all Java
data-plane families, F9 activates Scheduler assignment and lease-takeover
samples, F10 activates Connection Test Executor outcomes, F11 activates
Console BFF request and render samples, F12 activates trusted-proxy HSTS
samples, F13 activates Controller reconcile duration, and Phase 10 verifies
the API Server multi-region scrape path. Business observations for the
remaining API Server, Controller lifecycle, and auth-library descriptors
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
