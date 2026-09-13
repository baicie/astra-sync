# Phase 52 - Data-Plane Worker Identity in Logs

## Status

**Complete.**

Phase 52 adds `worker_id` to scoped data-plane logs so multi-Worker execution
can be correlated with the existing Worker metric label.

ADR: [ADR-101](../adr/adr-101-data-plane-worker-id-log-field.md)

---

## Goal

```text
RemoteBatchWorker -> worker_id + tenant_id + job_id
InProcessBatchWorker -> worker_id + tenant_id + job_id
Checkpoint -> worker_id + epoch + stage=checkpoint
```

Process-wide Coordinator logs do not emit `worker_id`.

---

## Delivered Files

```text
engine/src/main/java/io/astrasync/engine/observability/
├── DataPlaneLogContext.java
└── DataPlaneLogContextTest.java

engine/network/src/main/java/io/astrasync/engine/network/
└── RemoteBatchWorker.java

engine/network/src/test/java/io/astrasync/engine/network/
└── WorkerNetworkTest.java

engine/worker/src/main/java/io/astrasync/engine/worker/
└── InProcessBatchWorker.java

engine/worker/src/test/java/io/astrasync/engine/worker/
└── InProcessBatchWorkerTest.java

docs/adr/
└── adr-101-data-plane-worker-id-log-field.md

docs/phase52/
└── README.md
```

---

## Verification

- `DataPlaneLogContextTest` verifies worker field set/restore behavior.
- `InProcessBatchWorkerTest` verifies all task events carry the Worker ID.
- `WorkerNetworkTest` verifies remote dispatch events carry the Worker ID.
- Existing metric and protocol behavior is unchanged.

---

## Acceptance Criteria

- [x] Worker context accepts an optional bounded Worker ID.
- [x] Worker task and stage logs carry `worker_id`.
- [x] Remote dispatch and checkpoint logs carry `worker_id`.
- [x] Process-wide Coordinator logs omit `worker_id`.
- [x] MDC restore includes the Worker ID.
- [x] ADR-101 is indexed in `docs/adr/README.md`.
- [x] CHANGELOG includes the Phase 52 entry.

---

## Non-Goals

- No protocol, metric, or label change.
- No deployment or Helm change.
- No trace or `request_id` propagation.

---

## Rollback

Remove the worker-scoped context overload and Worker ID assignments. Existing
structured log fields and runtime behavior remain unchanged.
