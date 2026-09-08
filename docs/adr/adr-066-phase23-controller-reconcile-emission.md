# ADR-066: Phase 23 Controller Emission Sub-Slice 43.3.5 — controller_job_state_total via reconcile boundary

## Status

Accepted

## Context

ADR-058 §2 documents the `controller_job_state_total` counter with the
following call site:

> Controller reconcile boundary, at the durable state-transition commit
> (post-`repository.UpdateJobStatus`); `from_state` / `to_state` derived
> from before/after `repository.ReadJobStatus` snapshot; `tenant_id`
> derived from the SyncJob resource label or annotation (ADR-053 /
> ADR-029).

ADR-061 §129 and ADR-063 §3 confirm this as a Phase 22+ candidate
with the open question: "controller reconcile path needs durable commit
decision." ADR-053 §"Production reconcile safety" already establishes
that `r.Jobs.Update(...)` returning nil is the durable commit boundary
for the controller reconcile loop (controller-runtime writes the
Kubernetes resource to etcd, which is the durable store).

The `controller_job_state_total` metric has been **Recorder-wired**
since Phase 17 slice 43.3 (ADR-058). The `ObserveStateTransition`
method exists in `controller/internal/metrics.Recorder` and routes all
label values through `io.astrasync/control-plane/observability/normalize`.
The production **call site** — the reconcile loop — is the missing piece.

The `SyncJobReconciler.converge` method is the durable commit boundary:
every `r.Jobs.Update(ctx, next, stored.Version)` that returns `nil`
represents a state machine transition that has been written to the job
repository (backed by etcd / PostgreSQL). At that point:

- `stored.Status.State` is `fromState` (the state before the transition)
- `next.Status.State` is `toState` (the state after the transition)
- `stored.Status.State != next.Status.State` iff a transition occurred

The `tenant_id` label is derived from the Kubernetes SyncJob resource
label `astrasync.io/tenant-id`. The controller's `Reconcile` method
receives the `*syncv1.SyncJob` resource which carries this label. If
the label is absent, `_unknown` is emitted (enforced by
`normalize.NormalizeTenant`).

The namespace label (`namespace` in the Prometheus label, distinct from
the Kubernetes `Namespace` field) comes from `resource.Namespace` —
the Kubernetes namespace that scopes the SyncJob.

## Decision

### Slice 49.1: Tenant label documentation in SyncJob CRD

In `control-plane/controller/api/v1/syncjob_types.go`, add a godoc
comment to the `SyncJob` type documenting the required label:

```go
// SyncJob represents a data synchronization job managed by the AstraSync
// controller. The following labels are required for observability:
//
//   - astrasync.io/tenant-id: canonical lowercase UUID of the tenant that
//     owns this job. Used by controller_job_state_total to label job state
//     transitions per tenant. If absent, the controller emits _unknown.
```

No kubebuilder markers are added because Kubernetes does not enforce
label presence for custom resources; the API server is responsible for
setting the label at creation time. A future slice (49.1.5) will add
kubebuilder validation to enforce the label.

### Slice 49.2: Wire ObserveStateTransition in the converge loop

In `control-plane/controller/internal/controller/syncjob_controller.go`,
the `SyncJobReconciler` struct is extended with a `Metrics Recorder`
field (following the same pattern as the existing `ReconcileMetrics`
interface). The field is populated by `SetupWithManager` from the
controller-runtime manager's registerer.

The converge method returns a `job.Job`. After a successful
`r.Jobs.Update(ctx, next, stored.Version)` that returns `nil` with
`next.Status.State != stored.Status.State`, the recorder is called:

```go
tenantID := resource.Labels["astrasync.io/tenant-id"]
if tenantID == "" {
    tenantID = "_unknown"
}
r.Metrics.ObserveStateTransition(
    tenantID,
    resource.Namespace,
    string(stored.Status.State),
    string(next.Status.State),
)
```

The call site is after the `r.Jobs.Update` call returns `nil`, which
is the durable commit boundary. The call site is inside the `converge`
method, which is called by `Reconcile` — the recorder is called before
returning from `converge`.

For the deletion path (`reconcileDeletion`), the same pattern applies:
after `r.Jobs.Update(ctx, next, stored.Version)` succeeds for a
`RequestStop` transition, the recorder is called.

The `namespace` Prometheus label (distinct from Kubernetes `Namespace`)
is always `resource.Namespace` from the SyncJob resource.

The `from_state` / `to_state` values are bounded to the documented
Job state machine by `normalizeStateValue` in the metrics package:
values longer than 32 characters or not in the machine collapse to
`_unknown`.

### Slice 49.3: Metrics catalog update

`docs/observability/metrics-catalog.md` `controller_job_state_total`
row status: change from

> "Recorder wired in Phase 17 slice 43.3 (ADR-058); reconcile-path
> wiring pending (post-`repository.UpdateJobStatus`)"

to:

> "Recorder wired in Phase 17 slice 43.3 (ADR-058); Phase 23 slice 49
> (ADR-066) observes at the controller reconcile boundary. The call site
> is after `r.Jobs.Update(...)` returns nil — the durable commit point
> for the reconcile loop. `tenant_id` is read from the SyncJob resource
> label `astrasync.io/tenant-id`; if absent, _unknown is emitted.
> `namespace` is the Kubernetes namespace of the SyncJob resource.
> `from_state` / `to_state` are the job state before and after the
> transition, bounded by the state machine allowlist in the metrics
> package."

## Non-Goals

- `controller_epoch_fence_total` emission is **not** in scope for this
  slice. The epoch-fence outcome (`success|fenced|failure`) is emitted
  when the Scheduler reports a fence response; the controller reconcile
  path does not yet have a durable commit signal for fence responses
  (ADR-053 §3 open question). A future slice (49.3.5) will add this.
- Adding `TenantID` to the `job.Job` domain model is **not** in scope.
  The `tenant_id` is derived from the Kubernetes resource label, not
  from the job repository. The job domain model is unchanged.
- The API server setting the `astrasync.io/tenant-id` label at job
  creation is **not** in scope. This is a future slice (49.1.5).
- Slice 43.1.5 (api-server sign-in handler) and slice 43.2.5
  (auth-session-revoke) are Phase 24+ candidates per ADR-063 §3.

## Consequences

### Positive

- `controller_job_state_total` moves from "Recorder wired" to
  "emitted" in the metrics catalog, completing the Phase 17 activation
  matrix for the controller family.
- The controller reconcile loop now has a measurable state-transition
  signal: operators can alert on job state change rate per tenant /
  namespace.
- The call site design (after `r.Jobs.Update` returns nil) ensures
  the metric is emitted only for **durable** transitions, not
  intermediate states that are later rolled back.
- The K8s resource label as the tenant source is the standard
  multi-tenancy pattern for Kubernetes controllers (cf.
  `pod-template-hash`, `controller-uid`). The label is not enforced
  today but is documented as required.

### Negative

- If the API server does not set `astrasync.io/tenant-id` on SyncJob
  resources (slice 49.1.5 not yet done), all emissions carry
  `_unknown` as the tenant label. This means per-tenant alerting is
  not yet possible until the API server wiring is added. The catalog
  row documents this limitation.
- The `namespace` Prometheus label (K8s namespace) is not the same as
  the tenant. A single tenant may span multiple Kubernetes namespaces.
  This is a known limitation of the K8s-native observability approach
  (ADR-053 §5); a future ADR may introduce a `tenant_namespace` label
  that joins K8s namespace to tenant.
- The deletion path (`reconcileDeletion`) emits a transition
  (e.g., `RUNNING` → `CANCELED`) when a job is deleted. This is
  consistent with the state machine semantics but may be unexpected
  for dashboards that filter on non-terminal states.

## Alternatives Considered

### Tenant ID stored in the job domain model

Reject. This requires adding `TenantID string` to the `job.Job` struct,
updating `job.New(key, uid, tenantID, spec, now)`, updating the
`job.Repository` interface to return `TenantID`, updating the memory
and postgres repository implementations, updating every test that
constructs a `job.Job`, and updating the API server to extract
`TenantID` from the request context. The blast radius is large and
touches the job domain model, which is shared across the control
plane and the data plane's job compilation pipeline. The K8s label
approach achieves the same observability goal with a one-line
controller change.

### Observe at every state change (before durable commit)

Reject. The ADR-058 §2 call site is explicitly "at the durable
state-transition commit (post-`repository.UpdateJobStatus`)". Emitting
before the commit would produce a "transient" metric sample that is
rolled back if the commit fails (e.g., on a conflict). The counter
would overcount transitions that never happened.

### Use the Kubernetes namespace as the tenant_id directly

Reject. The K8s namespace is not guaranteed to be a canonical UUID
and is not guaranteed to map 1:1 to a tenant. A tenant may span
multiple namespaces; conversely, the K8s namespace name (e.g.,
`team-a`) does not pass `normalize.NormalizeTenant`'s UUID check
and would collapse to `_unknown`. The explicit `astrasync.io/tenant-id`
label is the correct source.
