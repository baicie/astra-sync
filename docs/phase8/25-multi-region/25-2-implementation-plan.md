# Phase 8 Slice 25.2: Implementation Plan

## Status

Implementation Complete (Phase 8, 2026-08-19). Matches the design decisions
in [`25-2-cross-region-channel.md`](25-2-cross-region-channel.md). See
[`docs/phase8/closeout.md`](../closeout.md) and the Phase 9 evidence in
[`docs/phase9/closeout.md`](../../phase9/closeout.md).

## Scope

This document is the implementation plan for Slice 25.2. It records
the decisions, dependencies, and verification path for the cross-region
gRPC channel with mutual TLS.

## Decisions Made

### 1. Channel Type

**Decision**: Bidirectional streaming RPC with mutual TLS

The channel uses a single bidirectional stream that carries three
event types (replication, topology, health). This simplifies reconnect
logic and ensures consistent ordering.

### 2. TLS Configuration

**Decision**: Reuse Phase 7 Slice 23 mTLS boundary

The channel uses the same mTLS configuration as the control-plane
API Server. A separate cross-region CA is used for the cross-region
certificates.

### 3. Identity

**Decision**: Region name as SPIFFE-style identity

Each region authenticates as itself. The certificate's SAN includes
the region name.

## Dependencies

| Dependency | ADR | Status | Notes |
|------------|-----|--------|-------|
| Slice 25.1 | ADR-048 | Complete | WAL transport and topology loader |
| Control-plane mTLS | ADR-045 | Complete | Required for TLS configuration |
| Slice 25.1 protobuf | — | Complete | Replication.proto |

## Implementation Tasks

### Phase 1: Channel Server

- [ ] Implement ChannelServer with bidirectional streaming
- [ ] Implement event router (replication / topology / health)
- [ ] Add mTLS configuration helpers
- [ ] Add server health check endpoint

### Phase 2: Channel Client

- [ ] Implement ChannelClient with reconnect logic
- [ ] Implement event acknowledger
- [ ] Add exponential backoff for retries
- [ ] Add connection state metrics

### Phase 3: Integration

- [ ] Wire channel to WAL writer (push replication events)
- [ ] Wire channel to topology watcher (push topology events)
- [ ] Wire channel to health reporter (push health events)
- [ ] End-to-end test with two-region topology

## Out-of-Scope

- Certificate rotation (Slice 25.5)
- Auto-promotion (Slice 25.3)
- Cross-region audit query (ADR-050)

## Verification

### Functional Verification

1. **Stream established**: gRPC bidirectional stream connects with mTLS
2. **Replication event**: WAL entry flows from primary to secondary
3. **Topology event**: Topology update flows between regions
4. **Health event**: Heartbeat flows between regions

### Security Verification

1. **TLS rejection**: Connection fails with mismatched CA
2. **Certificate expiry**: Connection fails with expired cert
3. **No anonymous**: Connection fails without client cert
4. **No plaintext**: Connection fails without TLS

### Resilience Verification

1. **Reconnect**: Stream resumes after interruption
2. **Buffer**: Events buffered during disconnection
3. **Ordering**: Events delivered in order after reconnect
4. **Backoff**: Exponential backoff on reconnect failures

### Performance Verification

1. **Latency**: < 50ms p99 same-region, < 200ms p99 cross-region
2. **Throughput**: 1000 events/second per channel
3. **Memory**: Bounded memory under disconnection

## Open Questions

None. All decisions are made in the design document.

## Rollout

1. **Staging**: Deploy to staging region pair with synthetic data
2. **Read-only**: Enable channel in read-only mode (no events)
3. **Replication**: Enable replication events through channel
4. **Full**: Enable all event types through channel