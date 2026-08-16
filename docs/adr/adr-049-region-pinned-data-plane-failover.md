# ADR-049: Region-pinned Data-Plane Failover with Epoch Fencing

## Status

Proposed (Phase 7 Slice 25 design cluster)

## Context

ADR-006 records that the Coordinator uses an epoch fencing token to
prevent split-brain writes: a stale Coordinator that tries to commit
to a sink after a new epoch is assigned is rejected by the Sink
Committer. ADR-010 records that a Job has one active execution
region at a time and that the failover sequence is "Detection →
Epoch Bump → Fencing → Recovery → Validation → Resume". ADR-027
records that a sink write must be transactional or idempotent.
ADR-009 records that delivery guarantees (Exactly/At-least/At-most)
must be based on real capability negotiation; the system must
forbid a silent fallback.

Phase 7 Slice 25 lifts the single-active-region constraint for the
control plane (ADR-048) while keeping the single-active-region
constraint for the data plane. The data-plane failover is the
boundary where ADR-006, ADR-009, and ADR-027 must hold across
regions. Without an explicit model, a region-pinned data plane can
either:

1. Write to the sink from the secondary region before the primary
   region's Coordinator is fenced (violates ADR-006).
2. Write to the sink without revalidating the sink's transactional
   or idempotent capability (violates ADR-009).
3. Re-execute a batch after the failover without a checkpoint that
   proves the last successful commit (violates ADR-027).

## Decision

The region-pinned data plane uses an **epoch-fenced, capability-
revalidated, checkpoint-coupled** failover model. The model has
three steps, each of which must succeed before the next:

### Step 1: Epoch Bump and Cross-Region Fencing

The promotion command (per the design document §"Failover Sequence
/ Operator decision") issues a new epoch in the secondary region's
etcd cluster. The new epoch is monotonically greater than every
epoch previously seen in either region. The new epoch is written
to the durable Job record (ADR-029) in the secondary region's
PostgreSQL, and the optimistic version is bumped together with the
epoch.

The new epoch is **fencing token for the data plane.** Every
Coordinator and Worker that the secondary region starts reads the
new epoch from the Job record before its first sink write. Every
sink commit request carries the new epoch. The Sink Committer
applies ADR-006's "If request.Epoch < currentEpoch: REJECT" rule
across regions:

- A Coordinator that is still running in the primary region (e.g.,
  due to a network partition that has not yet been detected by the
  secondary region) issues a sink commit with the old epoch. The
  Sink Committer rejects the commit and records an audit event of
  type `stale-epoch-commit-rejected`.
- A Coordinator that the secondary region starts after the epoch
  bump issues a sink commit with the new epoch. The Sink Committer
  accepts the commit.

The cross-region epoch ordering is enforced by the optimistic
version on the Job record (ADR-029), not by a shared etcd cluster.
The secondary region reads the last-replicated Job record and
inherits the last-seen epoch. The secondary region's local epoch
counter increments from that value.

### Step 2: Capability Revalidation

Before the secondary region's Coordinator issues its first sink
write, the Coordinator re-runs the capability negotiation
(ADR-009) against the sink. The revalidation is not optional: the
sink's transactional / idempotent capability must be re-confirmed
in the secondary region's network context.

The revalidation is bounded by `sinkRevalidationTimeout` (default
60 seconds, configurable via the Helm chart value
`multiRegion.sinkRevalidationTimeout`). A revalidation that times
out aborts the failover and surfaces an audit event of type
`region-promotion-aborted`. The operator may retry the promotion
after the sink is reachable.

The revalidation uses the existing capability-negotiation protocol
(ADR-009 §"Decision"). The protocol is unchanged; the multi-
region code path only adds the trigger.

### Step 3: Checkpoint-coupled Recovery

The secondary region's Coordinator recovers from the
last-replicated checkpoint (ADR-048 §"Checkpoint replication"). The
Coordinator restart uses the existing resumable full-load (ADR-024)
and checkpoint-coupled CDC (ADR-028) code paths unchanged.

The Coordinator does not issue a sink write until:

1. The recovery has reached a checkpoint that proves the last
   successful sink commit in the previous epoch (per ADR-027).
2. The capability revalidation (Step 2) has succeeded.
3. The new epoch is the only epoch that the secondary region's
   Sink Committer accepts.

A checkpoint that is partially replicated (i.e., the manifest is
replicated but the state-backend payload is not) is treated as a
non-replicated checkpoint. The Coordinator falls back to the
last-fully-replicated checkpoint and re-executes the uncommitted
batch. The re-execution is bounded by ADR-027's "transactional or
idempotent" rule: the sink must accept the re-execution without
duplication.

### Why Operator-initiated, Not Auto

The promotion command is operator-initiated (ADR-010). The
multi-region code path does not add auto-promotion. Auto-promotion
would couple the failure-detection threshold to a write-side
decision and would risk a "split-brain promotion" if the secondary
region's failure detector is faster than the primary region's
network recovery. The operator-initiated promotion preserves the
single-decision-maker boundary that ADR-029's optimistic-version
contract relies on.

## Consequences

### Positive

- The data-plane failover preserves ADR-006's epoch fencing. A
  stale Coordinator from the primary region cannot write to the
  sink after the secondary region has assumed control.
- The capability revalidation preserves ADR-009's "no silent
  fallback" rule. The secondary region cannot assume that the
  sink's transactional / idempotent capability is preserved
  across regions.
- The checkpoint-coupled recovery preserves ADR-027's
  "transactional or idempotent" rule. The Coordinator does not
  start a sink write without a checkpoint that proves the last
  successful commit.
- The model is consistent with the existing Phase 5 / Phase 6
  Coordinator and Worker code paths. The multi-region code path
  is a thin layer that triggers the existing capability
  negotiation and checkpoint-coupled CDC code paths.

### Negative

- The revalidation adds latency to the failover. The
  `sinkRevalidationTimeout` must be tuned against the deployment's
  network characteristics. A future operator may want to skip
  revalidation for sinks whose capability is statically known;
  the slice defers that decision.
- The cross-region epoch ordering relies on the optimistic-version
  conflict resolution (ADR-029). A buggy implementation that
  bypasses the optimistic-version check would risk a duplicate
  epoch assignment; the implementation slice must add a test that
  asserts the check is enforced.
- The operator-initiated promotion requires a human in the loop.
  The deployment's RTO is bounded by the operator's response time;
  the slice does not commit to a numeric RTO.

## References

- [ADR-006: Epoch Fencing for Coordinator](adr-006-epoch-fencing.md)
- [ADR-009: Exactly-Once via Capability Negotiation](adr-009-capability-negotiation.md)
- [ADR-010: Single Active Region per Job](adr-010-single-active-region.md)
- [ADR-024: Split-level Resumable Full-load Execution](adr-024-resumable-full-load.md)
- [ADR-027: Transactional or Idempotent Sink Commit](adr-027-transactional-idempotent-sink-commit.md)
- [ADR-028: Native CDC and Checkpoint-coupled Offsets](adr-028-native-cdc-and-checkpoint-coupled-offsets.md)
- [ADR-029: Durable Desired-state Job Lifecycle](adr-029-durable-desired-state-job-lifecycle.md)
- [ADR-048: Multi-Region Control-Plane Replication Model](adr-048-multi-region-control-plane-replication.md)
