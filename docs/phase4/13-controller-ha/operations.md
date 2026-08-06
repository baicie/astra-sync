# Phase 4 Slice 13 HA Operations

This runbook validates Controller and Scheduler failover without changing a durable execution
identity. Run disruption drills in a staging namespace with at least two ready Controller and
Scheduler replicas.

## Timing and invariants

- Controller takeover is bounded by its leader-election lease and retry periods.
- Scheduler takeover is bounded by `SCHEDULER_LEASE_DURATION` plus a reconciliation interval.
- `SCHEDULER_HEARTBEAT_TIMEOUT` must exceed two Coordinator heartbeat intervals and the Scheduler
  reconciliation interval.
- A takeover may change `owner_id` and increment `attempt`; it must not change `job_uid`,
  `execution_epoch`, the heartbeat token, or deterministic Kubernetes resource names.
- Kubernetes `Job.status.active` is not heartbeat evidence. Only an authenticated Coordinator POST
  advances `last_heartbeat_at`.

Inspect dispatch state without selecting the bearer token:

```sql
SELECT namespace, name, job_uid, execution_epoch, owner_id, phase, attempt,
       lease_expires_at, last_heartbeat_at, last_error
  FROM astrasync_scheduler_dispatches
 ORDER BY namespace, name, execution_epoch;
```

## Controller leader failover

1. Read the `astrasync-control-plane.sync.astrasync.io` Lease holder and identify the corresponding
   Controller Pod.
2. Delete that Pod while a SyncJob is `INITIALIZING` or `RUNNING`.
3. Confirm another Pod acquires the Lease within the configured lease window.
4. Confirm PostgreSQL `uid`, `version`, and `status.epoch` remain monotonic and the SyncJob status
   catches up within `controller.config.statusRefreshInterval`.
5. Change the active SyncJob spec while leaving `spec.state: RUNNING`. Confirm the old epoch moves
   through `CANCELING`, then the replacement spec starts exactly one higher epoch.

## Scheduler owner failover

1. Use the dispatch query to identify `owner_id`; with Helm this is the Scheduler Pod name.
2. Record UID, epoch, attempt, and the names of the matching Coordinator Job and Worker resources.
3. Delete the owner Pod and leave the Coordinator running.
4. After lease expiry, confirm another Scheduler owns the same row and `attempt` increases once.
5. Confirm there is still one Coordinator Job for that UID/epoch and its Worker resources retain the
   same deterministic names. A valid Coordinator heartbeat may continue through any Scheduler Pod.

## Heartbeat-loss drill

1. Temporarily block only the selected Coordinator Pod's access to the Scheduler heartbeat Service;
   do not block Scheduler access to PostgreSQL or Kubernetes.
2. Confirm `last_heartbeat_at` stops while `lease_expires_at` can continue moving.
3. At timeout, confirm one Scheduler atomically changes the dispatch to `STOPPING` and PostgreSQL Job
   state to `FAILED` with reason `HeartbeatTimeout`.
4. Delete the fencing Scheduler during cleanup. Confirm a replacement resumes Stop and completes the
   dispatch as `FAILED` rather than creating a new execution.
5. Remove the temporary block and confirm the old token is rejected after `STOPPING`.

## Orphan cleanup drill

Create staging-only resources carrying
`app.kubernetes.io/managed-by=astrasync-scheduler`, a valid
`sync.astrasync.io/job-uid`, and `sync.astrasync.io/execution-epoch`. Verify that:

- active dispatch identities retain Secret, Service, StatefulSet, and Coordinator Job resources;
- terminal identities lose auxiliary resources but retain the Coordinator Job until TTL;
- identities absent from dispatch history lose all four resource types;
- unmanaged resources and resources with missing or malformed identity labels are untouched.

## Recovery signals

Investigate when `last_heartbeat_at` is older than the timeout on an active phase, a `STOPPING` row
outlives two lease windows, Controller readiness reports PostgreSQL unavailable, or more than one
Coordinator Job exists for one UID/epoch. Preserve the terminal Coordinator logs before TTL when
diagnosing `HeartbeatTimeout`; never rotate or print `heartbeat_token` as an operational workaround.
