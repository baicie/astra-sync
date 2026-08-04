# Slice 05 Design

## Runtime Flow

```text
immutable JobSpec
      |--------------------------|
      v                          v
Coordinator                 Worker StatefulSet
  compile JDBC plan           compile JDBC plan
  enumerate all ranges        materialize Source + Sink
  open progress manifest      execute bounded exchange
      |                          ^
      +-- split descriptor ------+
      <-- result or failure ------
      |
      +-- atomic completion --> persistent progress volume
```

`CoordinatorApplication` validates process configuration, parses and compiles the JobSpec, requires a
JDBC source and sink, and enumerates the complete `JdbcRangeSplitSource` plan. It creates one
`RemoteBatchWorker` per validated endpoint and delegates scheduling and persistence to
`ResumableBatchCoordinator`.

`RemoteTaskFactory` gives the protocol the split and execution limits without creating a JDBC
connection on the Coordinator. Its Source and Sink are fail-fast descriptors. `WorkerServer`
reconstructs the split and asks `JdbcWorkerTaskFactoryProvider` for a real task. The provider compiles
the same JobSpec, creates a range reader for that exact descriptor, creates a fresh JDBC Sink, and
uses the JobSpec batch limit plus the Worker exchange capacity.

## Configuration

Coordinator variables:

| Variable | Required | Default | Meaning |
|---|---:|---:|---|
| `ASTRASYNC_COORDINATOR_JOB_SPEC` | Yes | - | Mounted immutable JobSpec path |
| `ASTRASYNC_COORDINATOR_PROGRESS_DIR` | Yes | - | Durable manifest directory |
| `ASTRASYNC_COORDINATOR_WORKERS` | Yes | - | Comma-separated `worker-id@host:port` endpoints |
| `ASTRASYNC_COORDINATOR_WORKER_TIMEOUT_MS` | No | `30000` | Connect and response timeout |
| `ASTRASYNC_COORDINATOR_MAX_IN_FLIGHT_TASKS` | No | `1` | Client permits per Worker |
| `ASTRASYNC_COORDINATOR_MAX_IN_FLIGHT_BATCHES` | No | `1` | Per-task Worker exchange capacity sent over the protocol |

JDBC Worker additions:

| Variable | Required | Default | Meaning |
|---|---:|---:|---|
| `ASTRASYNC_WORKER_JOB_SPEC` | Yes | - | The same mounted JobSpec used by the Coordinator |
| `ASTRASYNC_WORKER_MAX_IN_FLIGHT_BATCHES` | No | `1` | Worker-local Source-to-Sink exchange capacity |

The standard provider class is
`io.astrasync.engine.worker.JdbcWorkerTaskFactoryProvider`. Existing Worker process variables from
Slice 04 still control identity, protocol port, task concurrency, queue capacity, and connections.
The Coordinator and Worker exchange capacities must match because the server rejects a materialized
task that changes submitted limits.

## Identity And Persistence

The Helm StatefulSet uses the pod name as `ASTRASYNC_WORKER_ID`. Its headless Service gives each pod a
stable hostname, so generated endpoint identity and location survive pod replacement. The Coordinator
rejects duplicate IDs before opening the progress store.

The JobSpec Secret and progress claim are externally owned. The chart does not synthesize credentials
or silently create ephemeral progress. Every Job retry and later Helm revision mounts the same claim.
The progress store remains single-writer; Kubernetes Job retries must not overlap, and separate
releases must not run the same job ID concurrently.

## Failure Semantics

A task result is persisted only after remote execution succeeds. Completions received before another
task fails remain durable. A later invocation enumerates the complete plan, rejects any drift, and
contacts Workers only for missing splits. A fully complete plan creates no remote task and does not
require Workers to be reachable after enumeration.

The manifest is downstream of JDBC commits. Losing a success response or the Coordinator before
manifest replacement can replay committed rows. This operational slice does not change the
at-most-once JobSpec contract or add idempotency, distributed transactions, or exactly-once recovery.
