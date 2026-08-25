# ADR-052: Cross-Region Checkpoint Push RPC

## Status

Accepted

## Context

The initial replication contract exposed `StreamCheckpoints` as a server-streaming RPC
that allows a secondary region to receive checkpoint events. The replication runtime,
however, owns an outbound event channel and must deliver checkpoint events to a peer
without publishing them back into the local API service. A server-streaming RPC cannot
be called by the primary as an event push operation.

The existing wire contract must remain compatible with deployed v1 clients. The
control-plane transport also requires the existing authentication interceptor to
remain the enforcement boundary for cross-region calls.

## Decision

Add `PushCheckpoint` as a new unary RPC on `ReplicationService`. The request carries a
`CheckpointEvent`; the response acknowledges the event sequence. The RPC is additive:
existing methods and field numbers remain unchanged, and older clients continue to
operate using `StreamCheckpoints`.

The primary region invokes `PushCheckpoint` through the injected channel event sender.
The receiving API service validates the request, admits the event through a durable
checkpoint admission record keyed by source region, target region, job ID, and WAL
sequence, publishes the event to its bounded local subscribers, and returns the
acknowledged WAL sequence. The admission record stores a content fingerprint and is
committed only after successful subscriber delivery. A retry of a committed sequence
with the same content returns the acknowledgement without republishing; a conflicting
payload returns `AlreadyExists`; an in-progress admission returns `ResourceExhausted`.
Queue backpressure releases an uncommitted admission so the caller can retry.
Authentication and authorization remain owned by the API server interceptor and the
cross-region permission policy.

## Consequences

The runtime no longer needs a local event-loopback implementation to represent outbound
cross-region delivery. Generated Go and Java protocol artifacts must be regenerated
when the contract changes. Deployments must roll out servers before clients so an older
server can continue serving existing methods while a newer client waits for the new RPC
to become available.

The existing server-streaming method remains for consumers that prefer a long-lived
subscription. Durable admission prevents duplicate local publication after successful
receipt, while WAL/checkpoint replay and promotion fencing remain responsible for
recovery across an incomplete delivery, process restart, or epoch transition. The
admission table is replication-owned metadata and must be migrated before the API
accepts push traffic.
