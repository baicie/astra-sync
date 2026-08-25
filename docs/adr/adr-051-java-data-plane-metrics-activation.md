# ADR-051: Java Data-Plane Metrics Activation (Phase 7 Slice 26 Follow-up)

## Status

Accepted (implements Phase 7 Slice 26 follow-up `26.F8`).

## Context

ADR-047 §"F7 API Server SLO instrumentation" closed the API Server
availability and audit-query SLI families. The metrics catalog reserves
seven `coordinator_*` and `worker_*` families for the Java data plane
(`docs/observability/metrics-catalog.md` §"Data plane metrics"). F8 activates
all seven families. `coordinator_spill_bytes_total` is sampled by the
Worker-local spillable exchange after a durable payload write has been
successfully enqueued.

The follow-up `26.F8` must:

1. Activate the seven reserved families in the runtime boundary so the
   `Freshness` and `Deliverability` SLI expressions can consume real
   samples.
2. Keep the existing CLI summaries and liveness output on `stdout`
   and `stderr` (`AGENTS.md` §4.1, `docs/phase7/26-observability-handbook/design.md`
   §"Java data plane migration").
3. Honour the cardinality boundaries in `metrics-catalog.md` §"Labels":
   `tenant_id`, `job_id`, `worker_id`, `outcome` must be bounded.
4. Ship the changes without modifying the Helm chart or the deployment
   `ServiceMonitor`. The Go control plane pattern is "opt-in via
   `METRICS_LISTEN_ADDRESS`", and the Java data plane adopts the same
   default-disabled contract.

The architecture baseline (`AGENTS.md` §1) forbids silent degradation.
The decision therefore preserves three architectural invariants:

- **No new storage backend.** Metrics only flow through Micrometer
  in-process state plus an opt-in HTTP exposition; existing checkpoint
  durable storage remains untouched.
- **No protocol change.** The existing `WorkerProtocol` messages
  remain unchanged; metrics are sampled at the in-process Worker and
  Coordinator boundaries where the protocol identity is already
  validated.
- **Bounded cardinality.** Labels are derived only from validated
  protocol fields or fixed constants; no caller input becomes a label
  directly.

## Decision

### Stack

The Java data plane adopts **Micrometer 1.17.0** with
`micrometer-registry-prometheus` (Prometheus Java client 1.x) as the
metric registry, matching the Go control-plane contract that emits
Prometheus text with optional OpenMetrics negotiation. Versions live in
the root `pom.xml` `<properties>` (`micrometer.version`) following
`AGENTS.md` §4.1 rule 9; no module hardcodes a Micrometer version.

The `engine` artifact (`astrasync-engine`) owns the shared
`DataPlaneMetrics` API and the `DataPlaneMetricsServer`. Coordinator
and Worker artifacts depend on the shared module but do not import
Micrometer classes directly.

### Component name

All metric names use the prefixes already locked by
`docs/observability/metrics-catalog.md` §"Naming convention":

- `coordinator_*` — recorded by `DataPlaneMetrics` from inside
  `CheckpointBatchCoordinator` and the resumable `BatchCoordinator`
  paths that already wrap a checkpoint or a resumable full-load run.
- `worker_*` — recorded by `DataPlaneMetrics` from inside
  `InProcessBatchWorker` and `WorkerServer` checkpoint paths. The
  remote `RemoteBatchWorker` records at the Coordinator side because
  the Worker protocol message today does not carry a job identity; the
  Recorder only observes job-scoped series for checkpoint tasks.

### First-activation families

The activation implements all seven families reserved by the catalog:

| Metric | Sampler |
|---|---|
| `coordinator_batch_size_records` | Recorded in `CheckpointBatchCoordinator.run` for each dispatched split, value = `task.maxBatchRecords`. |
| `coordinator_batch_duration_seconds{stage}` | Recorded in `CheckpointBatchCoordinator.run` for each split, value = wall-clock from `run` entry until the `WorkerResult` is observed, `stage = "read"`. |
| `coordinator_checkpoint_duration_seconds` | Recorded in `CheckpointBatchCoordinator.run` for each `record(...)` call, value = nanos spent on the durable write. |
| `worker_records_read_total` | Recorded in `InProcessBatchWorker.produce` once per non-empty batch. |
| `worker_records_written_total` | Recorded in `InProcessBatchWorker.consume` (and the checkpoint path) once per non-empty batch. |
| `worker_records_rejected_total{reason}` | Recorded in `InProcessBatchWorker` when `consume` catches a non-exchange sink failure. The `reason` is the stable `SyncStage` code (`SINK_WRITE` or `SINK_OPEN`) per the catalog rule. |

| `coordinator_spill_bytes_total` | Recorded by the Worker-local `SpillableBatchStorage` after `SpillFrameCodec` bytes are durably written and successfully enqueued; it uses the established Coordinator semantic name and is exposed through the Worker process registry. |

### Label cardinality and tenant handling

`tenant_id` is fixed to the constant `_unknown` for `26.F8`. The Java
data plane currently receives no trusted tenant identity in the
`WorkerProtocol` or in `JobSpec`. Using `_unknown` matches the
auth/audit catalogue rule that prevents arbitrary caller input from
becoming a series. ADR-036 governs tenant identifiers in the control
plane; the Java data plane defers tenant labelling to a follow-up
slice that wires the trusted tenant UUID through the protocol.

`job_id` is derived from one of:

- `CheckpointExecutionContext.jobId()` for the checkpoint path
  (validated by `WorkerServer` against the protocol message, see
  `WorkerServer.executeCheckpoint`).
- The `BatchCoordinator.run(jobId, ...)` argument for the resumable
  Coordinator path.
- The fixed value `_unknown` for the non-checkpoint
  `BatchCoordinator.run(...)` overloads, which today lack a job
  identity.

The recorder normalises a non-canonical-lowercase UUID value to
`_unknown`, matching the API Server F7 rule.

`worker_id` is derived from `InProcessBatchWorker.workerId()` or the
remote worker id validated by `WorkerServer`.

`stage` and `reason` are taken from a fixed allowlist
(`read` / `SINK_WRITE` / `SINK_OPEN` / `SINK_CLOSE`) so the label set
is bounded.

`outcome` is the existing `success` / `failure` allowlist for
`coordinator_checkpoint_duration_seconds`, mirroring the API Server
contract.

### Endpoint contract

`DataPlaneMetricsServer` binds an HTTP listener only when
`METRICS_LISTEN_ADDRESS` is non-empty. The endpoint returns Prometheus text
format on `/metrics`. OpenMetrics content negotiation is deferred until the
registry endpoint needs exemplars.

When the environment variable is empty or absent, the recorder is
constructed but the server is not started. Existing operational
behaviour is unchanged.

The executor layer owns shutdown. `DataPlaneMetricsServer.close()`
stops the HTTP server with a bounded grace period (5 seconds) and the
shutdown sequence follows the reverse construction order defined in
`AGENTS.md` §4.1 rule 5.

### Dependency layout

- Root `pom.xml` adds `micrometer.version` and
  `micrometer-bom` import under `<dependencyManagement>`.
- `astrasync-engine` declares
  `io.micrometer:micrometer-core`,
  `io.micrometer:micrometer-registry-prometheus`. The endpoint uses the JDK
  HTTP server already available in Java 21.
- `coordinator` and `worker` depend on `astrasync-engine` (already
  declared in their `pom.xml`).
- No connector, format, or transform module imports Micrometer.

### Verification

The slice is verified by:

- Unit tests on `DataPlaneMetrics` asserting the label allowlist, the
  `_unknown` fallback for non-canonical UUIDs, and the recorder
  rejecting caller-supplied label keys that are not in the catalogue.
- Unit tests on `CheckpointBatchCoordinator` and
  `InProcessBatchWorker` that drive the public APIs and assert the
  `SimpleMeterRegistry` exposes the expected families and sample
  values, including:
  - happy path (batches read and written, checkpoint stored),
  - sink failure path (`worker_records_rejected_total{reason="SINK_WRITE"}`).
- A unit test that `DataPlaneMetricsServer` binds on
  `METRICS_LISTEN_ADDRESS` set and does not bind when the variable is
  absent.
- `make check` (Spotless, Go vet) and `make test` (Java + Go).

The metrics remain gated behind the catalog status table; a scrape
before the first sample simply exposes the registered descriptors with
zero series, which matches the documented "descriptor-only / pending"
semantics.

## Consequences

- Seven Java data-plane families become live; the SLO handbook's `Freshness`
  and `Deliverability` expressions can consume batch/checkpoint, spill-byte,
  and record-count samples. `coordinator_spill_bytes_total` measures only
  encoded payload bytes that were durably written and enqueued. It does not
  count failed writes, filesystem metadata, capacity release, or cleanup.
- `tenant_id` stays at `_unknown` for the data plane. The follow-up
  slice that trusts tenant identity in the Worker protocol will update
  the catalog status, refresh the ADR boundary, and possibly tighten
  the SPI.
- Operators continue to opt in to scrape by exporting
  `METRICS_LISTEN_ADDRESS`; no Helm or Docker change is required for
  the default-disabled contract.
- `coordinator_spill_bytes_total` is semantically a Coordinator metric but is
  sampled where spill executes: the Worker-local exchange. The Worker process
  exposes it through the existing opt-in `METRICS_LISTEN_ADDRESS` endpoint.
  Non-checkpoint exchange work has no trusted job identity, so its
  `tenant_id` and `job_id` labels remain `_unknown` until a protocol-backed
  identity slice is accepted.
- The Java executables do not migrate to SLF4J; the existing CLI
  summaries and liveness output are unchanged. The follow-up slice
  `26.F9` can address that migration independently of metric
  activation.

## Alternatives considered

- **Use the Prometheus Java simpleclient instead of Micrometer.**
  Rejected. The Go control plane and the catalogue both target
  Micrometer's `PrometheusMeterRegistry` semantics; using Micrometer
  keeps the descriptor naming, exemplar behaviour, and histogram
  handling consistent with `metrics-catalog.md`.
- **Activate the metrics endpoint by default in development.**
  Rejected. `AGENTS.md` §1 invariant 5 requires explicit, bounded
  resource surface. Operators must opt in to expose the data-plane
  port so a misconfigured deployment cannot silently publish
  per-tenant batch counts.
- **Track tenant_id from the Worker protocol.** Deferred. The current
  protocol carries only the worker id and the split id; introducing a
  tenant identity requires a protocol version bump that ADR-023
  governs. F8 keeps `_unknown` and lets `26.F9` carry the protocol
  change.
- **Sample `coordinator_batch_duration_seconds{stage}` per stage.**
  Deferred. The in-process Worker exposes only wall-clock batch
  duration today; per-stage breakdown requires instrumenting the
  produce and consume threads separately. F8 records the
  wall-clock `stage = "read"` value that SLO `Freshness` already
  consumes; per-stage breakdown remains future work.
