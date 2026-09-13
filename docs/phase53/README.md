# Phase 53 - Worker Admission Observability

## Status

**Complete.**

Phase 53 adds structured logs for Worker protocol admission rejections and
cancellation handling.

References:
[ADR-100](../adr/adr-100-data-plane-structured-log-context.md),
[ADR-101](../adr/adr-101-data-plane-worker-id-log-field.md)

---

## Goal

```text
invalid task / protocol / cancel request -> WARN with worker_id
duplicate active task                    -> WARN with worker_id
task capacity exhausted                  -> WARN with worker_id
cancellation requested                   -> INFO with worker_id
```

When a normal task request contains tenant/job identity, those fields are
attached to the admission log as well.

---

## Delivered Files

```text
engine/network/src/main/java/io/astrasync/engine/network/
└── WorkerServer.java

engine/network/src/test/java/io/astrasync/engine/network/
└── WorkerNetworkTest.java

docs/phase53/
└── README.md
```

---

## Verification

- `WorkerNetworkTest` verifies an invalid task request is rejected and emits a
  structured WARN event carrying `worker_id`.
- Existing execution, cancellation, backpressure, checkpoint, and admission
  tests remain unchanged.

---

## Acceptance Criteria

- [x] Protocol version mismatches emit bounded WARN logs.
- [x] Invalid task requests emit bounded WARN logs.
- [x] Duplicate active tasks emit bounded WARN logs.
- [x] Capacity rejection emits a bounded WARN log.
- [x] Cancellation handling emits a bounded INFO log.
- [x] Admission logs carry `worker_id` and available tenant/job identity.
- [x] CHANGELOG includes the Phase 53 entry.

---

## Non-Goals

- No protocol or response semantic change.
- No metric, dependency, Helm, or CRD change.
- No request body or connector detail logging.

---

## Rollback

Remove the admission log statements and context wrappers from `WorkerServer`.
Admission decisions and protocol behavior remain unchanged.
