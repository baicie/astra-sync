# Scheduler Admission and Coordinator Dispatch

Phase 4 Slice 12 turns a durable `StartJob` command into one Kubernetes execution and writes the
result back to the same PostgreSQL Job. The Scheduler is a long-running Go process with health on
port `8082`; it does not expose a user API.

## Runtime Flow

```text
StartJob -> PostgreSQL Job INITIALIZING, epoch N
                  |
                  v
       Scheduler capacity + lease claim
                  |
                  v
   immutable JobSpec Secret + Worker Service/StatefulSet
                  |
          Workers become ready
                  |
                  v
          one-shot Coordinator Job
                  |
       +----------+-----------+
       v                      v
  Complete                Failed/Stop
       |                      |
  FINISHED              FAILED/CANCELED
```

Every resource name is derived from `(job UID, epoch)`. Repeating reconciliation or taking over an
expired lease therefore gets the same Secret, StatefulSet, Service, and Job instead of creating a
second execution. JobSpec credentials stay in a Kubernetes Secret. Terminal Coordinator Jobs are
retained until their configured TTL; execution-scoped Workers and the Secret are removed.

## Configuration

The process requires `DATABASE_URL`, `SCHEDULER_PROGRESS_CLAIM`,
`SCHEDULER_COORDINATOR_IMAGE`, and `SCHEDULER_WORKER_IMAGE`. Helm supplies these and the pod identity.
The principal tuning variables are:

| Variable | Default | Purpose |
|---|---:|---|
| `SCHEDULER_MAX_CONCURRENT_JOBS` | `1` | Global active execution capacity across all Scheduler replicas |
| `SCHEDULER_LEASE_DURATION` | `30s` | Dispatch ownership lease |
| `SCHEDULER_RECONCILE_INTERVAL` | `5s` | Desired-state polling interval |
| `SCHEDULER_OPERATION_TIMEOUT` | `10s` | PostgreSQL/Kubernetes operation deadline |
| `SCHEDULER_WORKER_REPLICAS` | `2` | Dedicated Workers per admitted execution |
| `SCHEDULER_COORDINATOR_TTL_SECONDS` | `86400` | Retention of terminal Coordinator Jobs |

`GET /healthz` reports process health. `GET /readyz` checks PostgreSQL and Kubernetes API access.
The operation timeout and reconcile interval must both be shorter than the lease.

The Helm chart enables dynamic scheduling by default. It creates a progress PVC when
`scheduler.progress.existingClaim` is empty. The default `ReadWriteOnce` claim and capacity of one
are compatible; configure an RWX claim before increasing concurrent jobs across nodes. Dynamic
Scheduler mode is mutually exclusive with the legacy fixed-JobSpec `worker.enabled` and
`coordinator.enabled` mode.

## Lifecycle Semantics

- A new dispatch is admitted only for PostgreSQL `RUNNING/INITIALIZING` desired state.
- Active rows continue to consume capacity after lease expiry; another replica takes over that row
  rather than admitting a replacement.
- Worker readiness keeps the Job in `INITIALIZING`. An active Coordinator Job advances it to
  `RUNNING`.
- Kubernetes Job Complete and Failed conditions advance the same epoch to `FINISHED` or `FAILED`.
- `StopJob` moves the Job to `CANCELING`; the Scheduler deletes all execution resources and advances
  to `CANCELED` only after they are absent.
- Checkpointed Coordinators receive the control-plane epoch and idempotently reuse it on Kubernetes
  Job retries. Stale and skipped epochs are rejected by the checkpoint store.

## Current Boundary

The dispatcher supports executable inline connector options. A non-empty `connectionRef` is failed
before Kubernetes resources are created because catalog/Secret reference resolution is not yet
implemented. Worker and Coordinator resources are execution-scoped because the current Worker
protocol intentionally materializes tasks from one immutable mounted JobSpec.

Slice 13 owns controller-to-PostgreSQL convergence, execution heartbeats, orphan sweeps, failover
drills, and operational HA evidence. Slice 12 provides deterministic takeover but does not claim
continuous data-plane liveness detection or multi-region failover.

See [design.md](design.md), [implementation-plan.md](implementation-plan.md), and
[verification.md](verification.md).
