# Phase 55 - Request ID Propagation

## Status

**Complete.**

Phase 55 carries one Coordinator execution request ID through the Worker
protocol and structured logs, without emitting OpenMetrics exemplars.

References:
[ADR-102](../adr/adr-102-data-plane-request-id-propagation.md),
[ADR-100](../adr/adr-100-data-plane-structured-log-context.md)

---

## Goal

```text
Coordinator run -> one UUID -> execution logs
                         -> remote normal task request -> Worker task logs
                         -> checkpoint request       -> Worker checkpoint logs
old request       -> empty request_id -> omitted from logs
```

---

## Delivered Files

```text
protocol/worker-protocol/src/main/proto/v1/
└── worker.proto

engine/runtime/src/main/java/io/astrasync/engine/runtime/
└── BatchTask.java

engine/network/src/main/java/io/astrasync/engine/network/
├── RemoteBatchWorker.java
├── RemoteTaskFactory.java
├── WorkerProtocolMapper.java
└── WorkerServer.java

engine/worker/src/main/java/io/astrasync/engine/worker/
└── InProcessBatchWorker.java

engine/src/main/java/io/astrasync/engine/observability/
└── DataPlaneLogContext.java

engine/coordinator/src/main/java/io/astrasync/engine/coordinator/
└── CoordinatorApplication.java
```

---

## Verification

- `RuntimeContractsTest` verifies request identity defaults and immutable task
  rebinding.
- `RemoteTaskFactoryTest` verifies normal-task wire propagation.
- `WorkerNetworkTest` verifies trusted request identity materialization,
  structured logs, and old-request compatibility.
- `CheckpointNetworkTest` verifies checkpoint request identity propagation.
- `InProcessBatchWorkerTest` verifies success and failure log context.
- `CoordinatorApplicationTest` verifies one request ID spans execution start
  and completion.
- `DataPlaneLogContextTest` verifies `_unknown` is omitted and real values are
  restored.

---

## Acceptance Criteria

- [x] Normal and checkpoint Worker requests carry optional `request_id`.
- [x] Old requests remain compatible and omit the log field.
- [x] One Coordinator execution uses one request ID.
- [x] Worker task, stage, outcome, and checkpoint logs retain the request ID.
- [x] Protocol versions and metric labels remain unchanged.
- [x] No OpenMetrics exemplar is emitted.
- [x] CHANGELOG and ADR index include the Phase 55 entry.

---

## Non-Goals

- No authentication or authorization change.
- No metric label or exemplar change.
- No state, checkpoint, dependency, Helm, or CRD change.
- No validation error response change for empty request IDs.

---

## Rollback

Remove the appended protobuf fields and request ID context usage. Protocol
versions and all existing execution paths remain unchanged.
