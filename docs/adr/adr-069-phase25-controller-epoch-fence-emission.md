# ADR-069: Phase 25 Emission Sub-Slice 51.1 — controller_epoch_fence_total via reconcile boundary

## Status

Accepted

## Context

ADR-058 §2 documents the `controller_epoch_fence_total` counter with the
following call site:

> "Controller reconcile boundary, when the controller detects a new
> execution epoch on an active job; outcome `success|fenced|failure`"

ADR-066 §"Non-Goals" records this as Phase 23+ candidate pending the
durable commit decision for fence responses:

> "The controller reconcile path does not yet have a durable commit
> signal for fence responses (ADR-053 §3 open question). A future slice
> (49.3.5) will add this."

ADR-053 §"Decision" establishes the `checkpoint.Publisher` boundary and
confirms that promotion owns epoch fencing. The durable commit signal for
the controller reconcile path is `r.Jobs.Update(ctx, next, stored.Version)`
returning nil — the same boundary used for `controller_job_state_total`
in Phase 23 (ADR-066). ADR-053 does not introduce a new commit signal
for the controller; the existing durable commit is reused.

The `controller_epoch_fence_total` metric has been **Recorder-wired**
since Phase 17 slice 43.3 (ADR-058). The `ObserveEpochFence` method
exists in `controller/internal/metrics.Recorder` and routes all label
values through `io.astrasync/control-plane/observability/normalize`. The
outcome allowlist is `success|fenced|failure`; the `fenced` value is
exclusive to this metric and records a clean fence of an obsolete writer.
The production **call site** — the reconcile loop durable commit
boundary — is the missing piece.

The Phase 17 activation matrix for the controller family:

| Metric | Status |
|---|---|
| `controller_job_state_total` | Emitted (Phase 23 slice 49.2, ADR-066) |
| `controller_epoch_fence_total` | Recorder wired; production call site pending (this ADR) |

The `tenant_id` label is derived from the SyncJob resource label
`astrasync.io/tenant-id` (the same source as `controller_job_state_total`,
ADR-066 §"Slice 49.2"). The namespace label is the Kubernetes namespace
of the SyncJob resource.

## Decision

### Slice 51.0: ADR + index entry

This ADR + an entry in `docs/adr/README.md`.

### Slice 51.1: Fence detection in the converge loop

In `control-plane/controller/internal/controller/syncjob_controller.go`, the
`SyncJobReconciler` gains an `observeEpochFence(resource, stored, next)` helper.
The helper is called **after** a successful `r.Jobs.Update` returns nil
(the durable commit boundary, ADR-053 §3) when the epoch value changes:

```go
func (r *SyncJobReconciler) observeEpochFence(resource *syncv1.SyncJob, stored, next job.Job) {
    if r.Recorder == nil || r.Recorder.EpochFenceTotal == nil {
        return
    }
    tenantID := ""
    if resource != nil && resource.Labels != nil {
        tenantID = resource.Labels["astrasync.io/tenant-id"]
    }
    if tenantID == "" {
        tenantID = "_unknown"
    }
    outcome := "success"
    if stored.Status.Epoch != next.Status.Epoch {
        // A new epoch was assigned: the previous epoch's writer was fenced.
        // Emit "fenced" for clean fence, "failure" for unexpected.
        if next.Status.Epoch > stored.Status.Epoch {
            outcome = "fenced"
        } else {
            outcome = "failure"
        }
    }
    r.Recorder.ObserveEpochFence(tenantID, outcome)
}
```

The call site mirrors `observeTransition`: both are called after a
durable `r.Jobs.Update` returns nil, ensuring the metric is emitted only
for committed state changes. The `outcome` derivation:

- `fenced`: `next.Status.Epoch > stored.Status.Epoch` — a strictly higher
  epoch was assigned, meaning the previous epoch's writer was cleanly
  fenced off by the lease-fenced scheduler dispatch (ADR-030).
- `success`: `stored.Status.Epoch == next.Status.Epoch` — the epoch is
  unchanged, meaning this update did not involve a fence event.
- `failure`: `next.Status.Epoch < stored.Status.Epoch` — a lower epoch
  was written, which should not happen in normal operation. This is a
  safety fallback for misconfiguration; the allowlist collapses any
  unexpected value to `failure` (ADR-058 §3).

The helper is called in three sites inside `converge`:

1. **Spec-change stop path** (line after `r.Jobs.Update` for
   `RequestStop`): the job transitions from active to stopped; the
   previous epoch's writer is fenced.
2. **Desired-state transition path** (line after `r.Jobs.Update` for
   `RequestStart` or `RequestStop`): the epoch may change.
3. **Deletion path** (`reconcileDeletion`): the job is stopped before
   deletion; the previous epoch's writer is fenced.

All three sites use the same durable-commit pattern: the helper is
called only when `r.Jobs.Update` returns nil (no conflict).

### Slice 51.2: Metrics catalog update

`docs/observability/metrics-catalog.md` `controller_epoch_fence_total` row
status: change from

> "Phase 17 slice 43.3 (ADR-058) wired the Recorder; Phase 23 slice 49
> (ADR-066) wires the reconcile-boundary production call site.
> `controller_epoch_fence_total` remains Recorder-wired only
> (Phase 23+ candidate for slice 49.3.5)"

to:

> "Phase 17 slice 43.3 (ADR-058) wired the Recorder; Phase 25 slice 51
> (ADR-069) observes at the controller reconcile durable-commit boundary
> (`r.Jobs.Update` returning nil). The `outcome` label derives from the
> epoch change direction: `fenced` for a strictly higher epoch
> (`next.Status.Epoch > stored.Status.Epoch`), `success` for unchanged
> epoch, `failure` for a lower epoch (safety fallback). The `tenant_id`
> label is read from the SyncJob resource label
> `astrasync.io/tenant-id`; if absent, `_unknown` is emitted."

## Non-Goals

- The `epoch_fence` detection logic is purely in-memory (comparing
  `stored.Status.Epoch` vs `next.Status.Epoch`). No new fields are
  added to `job.Status` or the SyncJob CRD.
- The `controller_epoch_fence_total` metric is emitted at the controller
  reconcile boundary, not at the worker or scheduler boundary. A future
  ADR can add a separate counter for the scheduler's own fence decisions.
- The Phase 17 activation matrix's other pending emission (Java 26.F9
  data-plane metrics, ADR-051 §7) is not in scope.
- The API server setting `astrasync.io/tenant-id` on SyncJob resources
  (Phase 23 slice 49.1.5) is not in scope.

## Consequences

### Positive

- `controller_epoch_fence_total` moves from "Recorder wired" to
  "emitted" in the metrics catalog, completing the Phase 17 activation
  matrix for the controller family.
- The controller reconcile loop now has a measurable fence signal:
  operators can alert on fence rate per tenant / namespace.
- The call site design (after `r.Jobs.Update` returns nil) ensures the
  metric is emitted only for **durable** epoch assignments, not
  intermediate states that are later rolled back.
- The `fenced` outcome explicitly records clean writer fence-off,
  distinguishing it from `success` (no fence) and `failure`
  (misconfiguration).

### Negative

- If the API server does not set `astrasync.io/tenant-id` on SyncJob
  resources (slice 49.1.5 not yet done), all emissions carry `_unknown`
  as the tenant label. This is the same limitation as
  `controller_job_state_total` (ADR-066 §"Negative").
- The epoch-fence signal is derived from the job status projection in the
  reconcile loop. It does not capture fence events that occur outside
  the controller's reconcile path (e.g., a worker self-fencing on
  detection of a newer epoch). A future ADR can introduce a separate
  counter for worker-initiated fences.
- A lower-epoch write (`next.Status.Epoch < stored.Status.Epoch`)
  triggers `outcome=failure`. This is a defensive fallback; in normal
  operation it should not occur.

## Alternatives Considered

### Emit `fenced` on every active-to-stopped transition

Reject. Not every active-to-stopped transition involves a new epoch.
For example, a job that is already stopped (epoch 1) and is asked to
stop again has no fence. The epoch comparison (`next > stored`) is the
correct fence signal because it captures only actual new-epoch assignments.

### Emit `fenced` unconditionally when `stored.Status.Epoch > 0`

Reject. A job may have `stored.Status.Epoch > 0` without an active
writer (e.g., a job that was manually stopped while running). A new
epoch is assigned only when the reconcile loop explicitly sets a new
epoch. The epoch comparison is the precise fence signal.

### Emit the metric in the scheduler instead of the controller

Reject. The ADR-058 §2 documented call site is the "Controller reconcile
boundary". The scheduler may have its own fence decisions, but the
controller's perspective (what was actually committed to the job
repository) is the authoritative durable signal.

## References

- ADR-012 — Strict Versioned JobSpec Boundary
- ADR-029 — Durable Desired-state Job Lifecycle
- ADR-030 — Lease-fenced Scheduler Dispatch
- ADR-053 — Checkpoint WAL and Promotion Recovery Orchestration
- ADR-058 — Observability Catalog Backlog (Phase 17 umbrella)
- ADR-066 — Phase 23 Controller Emission (controller_job_state_total)
- ADR-068 — Phase 24 API Server Session Revoke Emission
