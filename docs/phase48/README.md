# Phase 48 - Worker Protocol Trusted Identity Propagation

## Status

**Complete.**

Phase 48 closes the ADR-051 protocol follow-up by adding wire-compatible
`job_id` and `tenant_id` propagation to Worker task requests and applying the
trusted identity to Java data-plane metrics.

ADR: [ADR-098](../adr/adr-098-worker-protocol-trusted-identity.md)

The Coordinator-side trusted tenant binding is supplied by Phase 49
([ADR-099](../adr/adr-099-coordinator-trusted-tenant-binding.md)).

---

## Goal

Carry trusted identity from the Coordinator request into the Worker task and
metric boundary:

```text
ExecuteTaskRequest       -> job_id, tenant_id
ExecuteCheckpointTaskRequest -> existing job_id, new tenant_id
```

Older clients omit both fields; empty values normalize to `_unknown`.

---

## Delivered Files

```text
protocol/worker-protocol/src/main/proto/v1/
└── worker.proto

engine/runtime/src/main/java/io/astrasync/engine/runtime/
└── BatchTask.java

engine/src/main/java/io/astrasync/engine/observability/
└── DataPlaneMetrics.java

engine/network/src/main/java/io/astrasync/engine/network/
├── RemoteTaskFactory.java
├── WorkerProtocolMapper.java
└── WorkerServer.java

engine/worker/src/main/java/io/astrasync/engine/worker/
└── InProcessBatchWorker.java

engine/coordinator/src/main/java/io/astrasync/engine/coordinator/
└── CoordinatorApplication.java

docs/adr/
└── adr-098-worker-protocol-trusted-identity.md

docs/phase48/
└── README.md
```

---

## Identity Contract

```text
trusted request identity
  -> BatchTask identity
  -> normalized tenant_id/job_id labels

missing or non-canonical identity
  -> _unknown
```

The normal task path sends the descriptor identity to the Worker. The
checkpoint path uses the existing trusted `job_id` plus the appended
`tenant_id`. The Worker validates the existing task structure and then binds
the request identity to the materialized task before execution.

---

## Verification

- `RuntimeContractsTest` verifies identity application without changing task
  resources.
- `DataPlaneMetricsTest` verifies trusted UUID labels and invalid-value
  collapse.
- `RemoteTaskFactoryTest` verifies identity is carried in the Worker request.
- `WorkerNetworkTest` verifies the server materializes the identity from the
  wire request.
- `CheckpointNetworkTest` verifies checkpoint identity propagation.
- `InProcessBatchWorkerTest` verifies normal and checkpoint metric labels.

---

## Acceptance Criteria

- [x] Existing protobuf field numbers are unchanged.
- [x] Identity fields are append-only and optional.
- [x] Normal and checkpoint requests carry tenant identity.
- [x] Normal requests carry job identity.
- [x] Missing identities normalize to `_unknown`.
- [x] Canonical lowercase UUIDs are emitted unchanged.
- [x] Non-canonical identifiers collapse to `_unknown`.
- [x] Existing protocol versions remain valid.
- [x] ADR-098 is indexed in `docs/adr/README.md`.
- [x] CHANGELOG includes the Phase 48 entry.

---

## Non-Goals

- No JobSpec tenant field or control-plane tenant-binding change.
- No authentication or authorization change to the Worker transport.
- No new metric family, label name, dependency, Helm value, or CRD.
- No exemplar emission.

---

## Rollback

Remove the appended protobuf fields and restore fixed `_unknown` identity at
the Worker metric boundary. Task execution, checkpoint protocol, and metric
names remain otherwise unchanged.
