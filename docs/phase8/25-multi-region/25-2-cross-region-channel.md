# Phase 8 Slice 25.2: Cross-Region gRPC Channel with mTLS

## Status

Implementation Complete (Phase 8, 2026-08-19). Implements the cross-region
gRPC channel described in
ADR-048 §"Cross-Region gRPC Channel" and ADR-049 §"Step 1: Epoch Bump
and Cross-Region Fencing".

## Context

ADR-048 records that the cross-region gRPC channel between the primary
region's API Server and the secondary region's API Server carries
replication events (not user requests). The channel inherits the
Phase 7 Slice 23 mutual-TLS boundary (ADR-045).

ADR-049 records that during failover, the secondary region issues a new
epoch via the durable Job record. The new epoch is the fencing token
for the data plane. The cross-region channel must carry the epoch
bump request and the fence acknowledgement.

Slice 25.1 implemented the WAL transport and topology loader. Slice
25.2 implements the channel that connects the two.

## Decision

The cross-region gRPC channel is a **bidirectional streaming RPC with
mutual TLS** that carries three event types:

1. **Replication events** — checkpoint WAL entries from primary to
   secondary regions.
2. **Topology events** — region configuration changes from any region
   to all peers.
3. **Health events** — region liveness status (bidirectional).

### Channel Contract

The channel uses the `StreamCrossRegion` RPC defined in
`replication.proto`. The stream carries:

```
message CrossRegionEvent {
  CrossRegionEventType type = 1;
  string source_region = 2;
  string target_region = 3;
  google.protobuf.Timestamp timestamp = 4;

  oneof payload {
    CheckpointEvent checkpoint = 10;
    TopologyUpdate topology = 11;
    HealthUpdate health = 12;
  }
}
```

### Mutual TLS

The channel uses the same mTLS configuration as the control-plane
API Server (ADR-045):

- TLS 1.3 minimum
- Server certificate signed by the cluster's CA
- Client certificate required
- Cross-region certificates signed by a **separate cross-region CA**
  managed by the operator
- Certificate rotation handled by the operator (not by the
  repository)

### Authentication and Authorization

Each region authenticates as itself. The channel uses the region name
as the SPIFFE-style identity. The operator configures the
cross-region CA and the per-region certificates.

The channel is **server-authenticated only on the data plane path**:
the secondary region's API Server trusts the primary region's
certificate, and vice versa. No user identities traverse the
channel.

### Failure Modes

| Failure | Behavior |
|---------|----------|
| TLS handshake fails | Channel rejected; secondary region refuses replication |
| Stream interrupted | Buffer events; resume from last acknowledged sequence |
| Region becomes unreachable | Mark region as unhealthy; operator receives audit event |
| Certificate expired | Channel rejected; replication halts; operator must rotate |

## Implementation

### New Go Packages

```
control-plane/
  replication/
    channel/
      server.go        # gRPC server for cross-region events
      client.go        # gRPC client for cross-region events
      tls.go           # mTLS configuration helpers
      stream.go        # Streaming event handler
```

### Configuration

```go
type ChannelConfig struct {
    Region         string
    PeerRegion     string
    PeerEndpoint   string
    CACertPath     string
    ClientCertPath string
    ClientKeyPath  string
    ServerName     string
}
```

### Verification

- **Functional**: gRPC stream established, events flow both directions
- **Security**: TLS handshake fails with mismatched CA, expired cert
- **Resilience**: Stream resumes after interruption
- **Performance**: Latency < 50ms at p99 in same-region, < 200ms at
  p99 cross-region

## Consequences

### Positive

- Cross-region replication uses existing mTLS boundary
- Channel is bidirectional, supporting health and topology events
- Failure modes are explicit and testable
- Channel does not couple to PostgreSQL topology

### Negative

- Cross-region certificate management is operator-owned
- Channel latency adds overhead to replication events
- Bidirectional stream requires careful reconnect logic

## References

- [ADR-045: Control-Plane Mutual TLS and Network Boundary](../adr/adr-045-control-plane-mtls-and-network-boundary.md)
- [ADR-048: Multi-Region Control-Plane Replication Model](../adr/adr-048-multi-region-control-plane-replication.md)
- [ADR-049: Region-pinned Data-Plane Failover with Epoch Fencing](../adr/adr-049-region-pinned-data-plane-failover.md)
- [Slice 25.1: PostgreSQL Replication Topology](25-1-replication-topology.md)