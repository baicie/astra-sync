# ADR-053: Checkpoint WAL and Promotion Recovery Orchestration

## Status

Accepted

## Context

Cross-region replication can only recover a job when a completed checkpoint has both a
validated manifest and a durable WAL record. The existing control-plane job status stores
checkpoint summaries, but it does not contain the manifest URI required by recovery. A
promotion also must not report failover complete before the target region has restored a
checkpoint under the new fenced epoch.

The runtime must remain independent of PostgreSQL and object-storage SDKs. Deployment
adapters own WAL persistence, checkpoint object access, epoch storage, and connection
capability negotiation.

## Decision

Introduce a `checkpoint.Publisher` boundary. A completed checkpoint producer supplies the
job ID, active epoch, manifest URI, and completion timestamp. The deployment-owned WAL
publisher verifies that the supplied epoch is still current before appending a WAL entry.
The producer must not synthesize a manifest URI from a checkpoint ID.

Promotion remains the owner of epoch fencing and capability revalidation. When a recovery
coordinator is injected, promotion invokes checkpoint-coupled recovery after the new epoch
is written and capability validation succeeds. Only a successful recovery permits the
`FailoverComplete` state. The domain manager retains an explicit standalone mode for local
callers that do not configure recovery orchestration.

Expose recovery through the additive `RegionRecoveryService.RecoverForPromotion` RPC. The
request includes the job, source and target regions, new epoch, promotion ID, and
idempotency key. API Server validation and an injected recovery backend own the RPC; the
runtime does not depend on generated API types outside its bridge layer.

Replication runtime enforces strictly increasing, gap-free WAL sequences. gRPC send errors
are retried only for transient resource or transport statuses; invalid arguments,
permission failures, failed preconditions, and conflicts fail the replicator and surface
through runtime readiness/state.

## Consequences

Checkpoint execution components must call the publisher only after the manifest is durable.
A PostgreSQL-backed outbox or another durable adapter can retry publication without making
runtime depend on a storage SDK. Recovery and promotion tests must provide explicit fakes
for both boundaries.

A target API Server must be upgraded before a client starts invoking the recovery RPC. Older
servers remain compatible with existing replication methods, while recovery orchestration
fails closed when the new backend is unavailable. The observability catalog reports recovery
attempts separately from promotion attempts.
