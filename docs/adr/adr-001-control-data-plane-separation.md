# ADR-001: Control Plane and Data Plane Separation

## Status

Accepted

## Context

The system needs to support long-running data synchronization jobs that must continue operating even when control plane components are being upgraded or experiencing temporary failures. Additionally, we need to ensure that control plane issues (e.g., API overload, database connection pool exhaustion) do not directly impact data throughput and latency.

## Decision

We adopt a strict separation between Control Plane and Data Plane:

**Control Plane** (Go):
- API Server: Job CRUD, connection management, schema browsing
- Controller: Kubernetes-style reconcile loop for job lifecycle
- Scheduler: Worker allocation and task scheduling
- Catalog: Metadata storage and schema registry

**Data Plane** (Java):
- Coordinator: Job execution coordination, checkpoint management
- Worker: Data reading, transformation, and writing
- State Backend: RocksDB-based state management
- Network: Netty-based data transfer

Control plane components communicate via:
- gRPC for control commands
- etcd for distributed coordination and leader election

Data plane components communicate via:
- Netty for high-performance data transfer
- gRPC for metadata and control messages

## Consequences

### Positive
- Data plane can continue running even when control plane is unavailable
- Independent scaling of control and data planes
- Different teams can work on control and data planes with minimal coordination
- Failure isolation prevents cascade failures

### Negative
- Increased deployment complexity
- Need for separate monitoring and alerting systems
- Two different programming languages require more diverse skill sets

## References

- [Flink Architecture](https://nightlies.apache.org/flink/flink-docs-release-2.2/docs/concepts/flink-architecture/)
- [SeaTunnel Architecture](https://seatunnel.apache.org/docs/concept/architecture/)
- [Kubernetes Controller Pattern](https://kubernetes.io/docs/concepts/architecture/controller/)
