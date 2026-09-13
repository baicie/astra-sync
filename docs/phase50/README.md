# Phase 50 - Data-Plane Structured Log Context

## Status

**Complete.**

Phase 50 attaches the stable data-plane identity and lifecycle fields defined
by `docs/observability/log-conventions.md` to Coordinator and Worker logs.

ADR: [ADR-100](../adr/adr-100-data-plane-structured-log-context.md)

---

## Goal

Emit bounded structured context without copying identity into every message:

```text
Coordinator: tenant_id, job_id, epoch, outcome
Worker task: tenant_id, job_id
Worker stage: stage=read|write
Checkpoint: epoch, stage=checkpoint
```

MDC values must be restored after nested scopes and after failures.

---

## Delivered Files

```text
engine/src/main/java/io/astrasync/engine/observability/
└── DataPlaneLogContext.java

engine/coordinator/src/main/java/io/astrasync/engine/coordinator/
└── CoordinatorApplication.java

engine/worker/src/main/java/io/astrasync/engine/worker/
└── InProcessBatchWorker.java

engine/src/test/java/io/astrasync/engine/observability/
└── DataPlaneLogContextTest.java

engine/worker/src/test/java/io/astrasync/engine/worker/
└── InProcessBatchWorkerTest.java

docs/adr/
└── adr-100-data-plane-structured-log-context.md

docs/phase50/
└── README.md
```

---

## Context Contract

```text
open(tenantId, jobId, epoch)
open(tenantId, jobId, epoch, stage, outcome)
close()
  -> restore the values present before open
```

The context is nested and thread-local. Blank fields and non-positive epochs
are not emitted. `request_id` remains separate.

---

## Verification

- `DataPlaneLogContextTest` verifies set, nested restore, blank suppression,
  and epoch handling.
- `InProcessBatchWorkerTest` verifies successful task events carry the trusted
  tenant and job identity.
- Coordinator integration logs show execution start/completion with tenant,
  job, epoch, and outcome fields.
- Existing error and metric tests remain unchanged.

---

## Acceptance Criteria

- [x] MDC values are scoped and restored.
- [x] Nested contexts preserve the outer stage.
- [x] Blank fields and non-positive epochs are ignored.
- [x] Coordinator start/completion logs carry tenant/job/epoch.
- [x] Worker task logs carry tenant/job identity.
- [x] Source and sink threads carry read/write stages.
- [x] Checkpoint logs carry epoch and checkpoint stage.
- [x] Failure logs carry the failure outcome.
- [x] ADR-100 is indexed in `docs/adr/README.md`.
- [x] CHANGELOG includes the Phase 50 entry.

---

## Non-Goals

- No `request_id` propagation or exemplar emission.
- No new log field name or type.
- No change to Logback appenders.
- No protocol, Helm, CRD, metric, or dependency change.

---

## Rollback

Remove context usage from Coordinator and Worker execution boundaries and
delete `DataPlaneLogContext` plus its tests. Existing operational logging
remains otherwise unchanged.
