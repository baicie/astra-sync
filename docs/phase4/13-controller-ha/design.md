# Phase 4 Slice 13 Design: Controller Convergence and HA

## Goals

1. Make PostgreSQL the lifecycle authority for every Kubernetes `SyncJob`.
2. Make Controller status eventually consistent with scheduler and data-plane progress.
3. Preserve optimistic version and execution-epoch fencing during API, Controller, and Scheduler
   races.
4. Detect an execution whose Coordinator no longer reports liveness independently of Scheduler and
   Kubernetes API health.
5. Remove leaked execution resources without deleting unrelated user resources.

## Authority and convergence

The CRD is the declarative interface. `spec.source`, `spec.sink`, `spec.transforms`, `spec.delivery`,
`spec.runtime`, and `spec.state` are imported into the PostgreSQL Job. PostgreSQL owns `uid`,
`version`, observed lifecycle state, epoch, restart count, checkpoint, and failure details.

On first observation the Controller creates a Job using the Kubernetes UID when it is a UUID; for
non-UUID test or migration identifiers it derives a stable UUIDv5 from the immutable object UID.
If the row already exists, the Controller never replaces its UID. It updates the JobSpec only when
the stored lifecycle is inactive, and uses `RequestStart`/`RequestStop` for desired-state changes.
Every update uses the version read immediately before it. `ErrConflict` and create races are
re-read with a short requeue, not overwritten.

The Controller mirrors the complete PostgreSQL status to the CRD status subresource. Every managed
SyncJob is requeued periodically because Scheduler and API writers update PostgreSQL directly and do
not emit Kubernetes watch events. This also recreates or reconverges inactive rows after out-of-band
PostgreSQL lifecycle changes.

## Deletion and finalizer

The Controller adds `sync.astrasync.io/control-plane-finalizer`. On deletion it requests STOPPED in
PostgreSQL and keeps the finalizer while the Job is active. Once the durable Job is terminal or
already absent, it deletes the inactive PostgreSQL row with an expected version and removes the
finalizer. A transient Kubernetes or PostgreSQL error leaves the finalizer in place for retry.

## Heartbeat and fencing

Dispatch rows store `last_heartbeat_at` separately from the ownership lease. Scheduler lease
renewal records orchestration progress but does not count as execution liveness. PostgreSQL creates
a random UUID `heartbeat_token` for each UID/epoch. The Kubernetes dispatcher writes it to the
immutable JobSpec Secret and injects both the execution-specific Scheduler URL and Secret reference
into the Coordinator. The Coordinator synchronously reports once before starting execution and then
posts periodically. The Scheduler endpoint accepts only the matching UID, epoch, token, and an active
dispatch phase. Kubernetes Job observations never update `last_heartbeat_at`.

Admission time initializes `last_heartbeat_at`, providing one timeout window for Worker and
Coordinator startup. `Claim` takes over another owner's row when its lease expires or heartbeat is
stale. The PostgreSQL advisory admission lock serializes ownership and capacity decisions. A stale
owner receives `ErrLeaseLost` on its next fenced write, while the new owner reuses the existing
identity and resources.

The reconciliation snapshot is only a fast timeout signal. Before changing Job state, Scheduler
atomically compares owner, lease, active phase, and heartbeat age and changes the dispatch to
`STOPPING`. A concurrent valid heartbeat makes that update affect zero rows and the execution remains
active. Once `STOPPING` is durable, heartbeats are rejected and any Scheduler owner can resume the
`HeartbeatTimeout` Job transition, resource Stop, and terminal dispatch update after a crash.

## Orphan sweep

Scheduler periodically lists all dispatch records and builds two keep sets. Active records keep
Secrets, Services, and StatefulSets. All known records, including terminal history, keep Coordinator
Jobs for post-mortem inspection. The dispatcher lists only resources with
`app.kubernetes.io/managed-by=astrasync-scheduler` and requires both
`sync.astrasync.io/job-uid` and `sync.astrasync.io/execution-epoch` labels to parse as a valid
identity. A terminal identity therefore loses auxiliary resources but retains its Coordinator Job;
an identity absent from dispatch history loses all execution resources. Missing or malformed labels
are ignored rather than inferred from resource names.

The sweep is best-effort and returns aggregated errors. It runs after normal reconciliation, so a
resource created by a current owner is present in the keep set before cleanup is considered.

## HA guarantees and limits

- Controller leader election prevents concurrent CRD writers; PostgreSQL versions still protect
  against API server and Scheduler updates.
- Scheduler replicas share advisory-lock admission, owner leases, authenticated heartbeat takeover,
  atomic timeout fencing, and immutable execution identities.
- A failover does not duplicate a Coordinator or Worker execution. It reuses deterministic resource
  names and verifies UID, epoch, and JobSpec fingerprint before adoption.
- A Kubernetes API outage can delay cleanup. The liveness timeout fails the durable execution and
  the orphan sweep removes only positively identified auxiliary resources after recovery.
- This slice remains single-region and does not provide cross-region active/active execution.
