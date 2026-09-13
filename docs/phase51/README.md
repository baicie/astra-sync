# Phase 51 - Remote Worker Task Log Context

## Status

**Complete.**

Phase 51 extends the Phase 50 structured log context to Coordinator-side
remote task dispatch.

Reference: [ADR-100](../adr/adr-100-data-plane-structured-log-context.md)

---

## Goal

Make every remote task lifecycle event joinable to the same tenant/job
identity used by Worker metrics and logs:

```text
RemoteBatchWorker task start     -> tenant_id, job_id
RemoteBatchWorker task complete  -> outcome=success
RemoteBatchWorker task failure   -> outcome=failure
Checkpoint dispatch              -> epoch, stage=checkpoint
```

---

## Delivered Files

```text
engine/network/src/main/java/io/astrasync/engine/network/
└── RemoteBatchWorker.java

engine/network/src/test/java/io/astrasync/engine/network/
└── WorkerNetworkTest.java

docs/phase51/
└── README.md
```

---

## Verification

- `WorkerNetworkTest` executes a real remote task through `WorkerServer` and
  verifies `RemoteBatchWorker` log events carry the trusted tenant/job fields.
- Existing remote execution, cancellation, backpressure, and checkpoint tests
  remain unchanged.
- Failure logs record only the bounded exception class name.

---

## Acceptance Criteria

- [x] Remote task start logs carry tenant/job identity.
- [x] Remote task completion logs carry `outcome=success`.
- [x] Remote task failures carry `outcome=failure`.
- [x] Checkpoint dispatch carries epoch and `stage=checkpoint`.
- [x] MDC context is restored after dispatch.
- [x] No protocol, metric, dependency, or deployment change.
- [x] CHANGELOG includes the Phase 51 entry.

---

## Non-Goals

- No new protocol identity field.
- No `request_id` or trace propagation.
- No metric or dashboard change.
- No Helm or Docker change.

---

## Rollback

Remove `DataPlaneLogContext` usage and log statements from
`RemoteBatchWorker`. Protocol and remote execution behavior remain unchanged.
