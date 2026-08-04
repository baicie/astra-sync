# Slice 04 Design

## Progress Model

`SplitPlan` canonicalizes the complete split set by split ID and hashes each descriptor and the full
plan. A `FullLoadProgress` manifest stores format version `1`, the job ID, the plan, and an ordered map
of completed splits. Each completion preserves the first successful Worker ID, split fingerprint, and
`SyncResult` metrics.

```json
{
  "formatVersion": 1,
  "jobId": "orders-load",
  "plan": {
    "fingerprint": "<sha-256>",
    "splitFingerprints": { "orders-0001": "<sha-256>" }
  },
  "completedSplits": {
    "orders-0001": {
      "workerId": "worker-a",
      "splitFingerprint": "<sha-256>",
      "metrics": { "readCount": 1000, "writtenCount": 1000 }
    }
  }
}
```

Job IDs are lowercase DNS labels. This gives manifest and lock files bounded, path-safe names. The
store serializes access with an exclusive file lock, writes and forces a temporary file in the target
directory, and atomically replaces the manifest. Unsupported atomic replacement fails the update and
retains the previous manifest.

## Execution Flow

1. Enumerate the complete immutable split set and compute its `SplitPlan`.
2. Open or create the job manifest. Reject any plan drift before task materialization.
3. Materialize only splits absent from `completedSplits`.
4. Schedule pending tasks across the fixed Worker set.
5. Consume successful results in completion order and atomically record each one.
6. On success, reload the manifest, require every planned split to be complete, and return results in
   original enumeration order with cumulative metrics.

`BatchCoordinator` retains one serial executor per Worker. A shared completion queue prevents a slow
earlier task from delaying persistence of a later successful task. The first task failure sets a latch;
tasks still queued behind it return without invoking their Worker. Already running tasks can still
finish, and a completion persisted before failure handling remains resumable.

## Failure Semantics

The store records completion only after `BatchWorker.execute` returns successfully. It does not record
partial metrics from a failed task. Restart never resumes inside a split: every unrecorded split starts
from its original source boundary.

There is an unavoidable replay window between external sink success and durable manifest replacement.
The sink must tolerate replay if duplicates are unacceptable. The file lock is not an HA protocol;
only one active Coordinator may execute a job, and there is no epoch token fencing stale Workers.

## Worker Runtime

`WorkerApplication` validates environment configuration, loads a deployment provider by class name,
starts `WorkerServer`, emits a readiness line, and blocks until shutdown. `WorkerService` owns the
server lifecycle. `WorkerHealthProbe` performs a bounded TCP connection to the actual protocol port.

| Variable | Required | Default | Meaning |
|---|---:|---:|---|
| `ASTRASYNC_WORKER_ID` | Yes | - | Unique protocol Worker identity |
| `ASTRASYNC_TASK_FACTORY_PROVIDER` | Yes | - | Provider implementation class |
| `ASTRASYNC_WORKER_PORT` | No | `50051` | Worker protocol listen port |
| `ASTRASYNC_MAX_CONCURRENT_TASKS` | No | `1` | Active task executor size |
| `ASTRASYNC_TASK_QUEUE_CAPACITY` | No | `0` | Bounded queued tasks; zero uses direct handoff |
| `ASTRASYNC_MAX_CONNECTIONS` | No | `16` | Simultaneous protocol connections |
| `ASTRASYNC_WORKER_HEALTH_HOST` | No | `127.0.0.1` | Container health probe target |

The provider receives an immutable snapshot of the process environment and returns the factory used
for every request. It must create fresh Worker-local Source and Sink resources for the requested split
and use execution limits consistent with the submitting Coordinator. The shaded JAR contains the
Worker runtime; provider code remains deployment-specific and is loaded from the plugin classpath.

## Deployment

The Worker image builds only the Worker reactor and runs as UID/GID 1000. Both its entry point and
Docker health check include `/app/plugins/*` on the classpath. The Helm deployment uses TCP liveness
and readiness probes against the protocol port, and a headless Service exposes ready Worker pod
addresses. Worker resources are not rendered by default; `worker.enabled=true` also requires
`worker.taskFactoryProvider`.
