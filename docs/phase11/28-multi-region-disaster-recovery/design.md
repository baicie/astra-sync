# Phase 11 Slice 28: Cross-Region Disaster Recovery Drill Design

## Context

Phase 9 proved checkpoint delivery, duplicate suppression, and secondary
restart behavior. Phase 10 proved that replication metrics reach the API
Server scrape endpoint. The remaining operational evidence is a bounded
primary outage drill that checks what the current runtime can prove and
rejects actions whose prerequisites are absent.

The Compose topology gives each region its own PostgreSQL database and object
store directory. It also renders a secondary process with the primary as its
standby target. Consequently, the secondary cannot promote itself through
the current static topology, and a checkpoint pushed through the replication
RPC is not by itself a durable checkpoint manifest and state set for recovery.

## Decision

Add one integration acceptance test that performs these steps:

1. Bootstrap both API Server regions.
2. Push and acknowledge a checkpoint sequence in the secondary service, then
   record replication status.
3. Stop the primary API Server and wait for both its HTTP and gRPC endpoints
   to become unavailable.
4. Read secondary topology and replication status while the primary is down.
5. Call promotion on the secondary and require `FailedPrecondition` because
   its configured target is not a standby region.
6. Call recovery with valid identifiers and require `NotFound` because the
   local recovery store has no checkpoint manifest for the drill job.
7. Start the primary and wait for both transport endpoints to recover.

The API service maps known promotion and recovery domain errors to stable gRPC
codes and replaces unknown backend details with sanitized messages. Context
cancellation and existing gRPC status errors remain intact.

## Scope Boundary

The drill does not alter the protobuf contract, Compose topology, object-store
layout, sink connector, capability catalog, or promotion policy. A future
phase may add a deployment-owned promotion harness after those prerequisites
are available; that work requires a separate architecture review if it
changes an invariant or ADR decision.

## References

- [ADR-010: Single Active Region](../../adr/adr-010-single-active-region.md)
- [ADR-048: Multi-Region Control-Plane Replication Model](../../adr/adr-048-multi-region-control-plane-replication.md)
- [ADR-049: Region-pinned Data-Plane Failover with Epoch Fencing](../../adr/adr-049-region-pinned-data-plane-failover.md)
- [Phase 9 closeout](../../phase9/closeout.md)
- [Phase 10 README](../../phase10/README.md)
