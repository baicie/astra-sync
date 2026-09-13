# Phase 44 - Data-Plane Batch Stage Metrics

## Status

**Complete.**

Phase 44 implements the read/write stage breakdown deferred by ADR-051.

ADR: [ADR-094](../adr/adr-094-data-plane-batch-stage-metrics.md)

---

## Goal

Record `coordinator_batch_duration_seconds` at the Worker source and sink
boundaries without introducing a new metric family, label, protocol field, or
deployment setting.

---

## Delivered Files

```text
engine/src/main/java/io/astrasync/engine/observability/
└── DataPlaneMetrics.java

engine/worker/src/main/java/io/astrasync/engine/worker/
└── InProcessBatchWorker.java

engine/src/test/java/io/astrasync/engine/observability/
└── DataPlaneMetricsTest.java

engine/worker/src/test/java/io/astrasync/engine/worker/
└── InProcessBatchWorkerTest.java

docs/adr/
└── adr-094-data-plane-batch-stage-metrics.md

docs/phase44/
└── README.md
```

---

## Metric Contract

```text
coordinator_batch_duration_seconds{tenant_id,job_id,stage}

stage=read   BatchSource.readBatch duration
stage=write  BatchSink.writeBatch duration
stage=unknown  any unsupported caller value
```

Checkpoint execution uses the trusted checkpoint `job_id`. Non-checkpoint
execution uses `_unknown`. `transform` is not emitted because no standalone
transform boundary exists in the distributed task contract.

---

## Verification

- `DataPlaneMetricsTest` asserts read/write acceptance and unknown-stage
  collapse.
- `InProcessBatchWorkerTest` asserts read/write timers for checkpoint and
  non-checkpoint execution.
- Existing worker count, checkpoint, spill, fencing, and failure tests remain
  unchanged.

---

## Acceptance Criteria

- [x] Source reads are timed as `stage=read`.
- [x] Sink writes are timed as `stage=write`.
- [x] Checkpointed execution retains the trusted job identifier.
- [x] Non-checkpoint execution uses bounded unknown identifiers.
- [x] Unknown stage values cannot widen the label set.
- [x] No new metric family, protocol field, dependency, or Helm change.
- [x] ADR-094 is indexed in `docs/adr/README.md`.
- [x] CHANGELOG includes the Phase 44 entry.

---

## Non-Goals

- No standalone transform instrumentation.
- No protocol or tenant-identity change.
- No new ServiceMonitor or metrics port.
- No histogram bucket change.

---

## Rollback

Remove Worker stage timing calls and restore the read-only stage allowlist.
Existing metric descriptors and labels remain compatible.
