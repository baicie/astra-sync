# ADR-071: Phase 27 — API Server / Console Sets `astrasync.io/tenant-id` on SyncJob CR

## Status

Accepted — §2 superseded by ADR-086; admission enforcement restored by ADR-087

## Context

The Phase 17 observability activation matrix (ADR-058) completed in
v0.8.0 (ADR-070). All six deferred metrics are now **emitted** in
production. However, three of those metrics carry `_unknown` as the
`tenant_id` label for the majority of SyncJob resources:

- `controller_job_state_total` (Phase 23, ADR-066): `_unknown` when
  `astrasync.io/tenant-id` is absent from the SyncJob CR.
- `controller_epoch_fence_total` (Phase 25, ADR-069): `_unknown`
  when absent.
- `apiserver_session_revoke_total` (Phase 24, ADR-068): `_unknown`
  or `_platform` when the principal has no active memberships.

The `astrasync.io/tenant-id` label is the standard multi-tenancy
contract for Kubernetes controllers (Phase 23 slice 49.1, ADR-066
§Key Design Decisions). The controller reads it from the SyncJob
resource in every reconcile loop and emits it as the `tenant_id`
Prometheus label. The label is currently optional; no validation
enforces its presence.

Phase 23 slice 49.1.5 was explicitly deferred ("Non-Goals
(Phase 23+)" in `docs/phase23/README.md`):

> Slice 49.1.5: API server setting `astrasync.io/tenant-id`
> label on SyncJob creation + kubebuilder validation

The Phase 23 slice 49.1 godoc documents the label as required but
does not enforce it. The SyncJob CRD godoc currently says:

> Labels are set by the API server at creation time; they are not
> enforced by the Kubernetes apiserver. A future slice will add
> kubebuilder validation markers to enforce the tenant-id label
> presence.

**This statement is incorrect.** The API server does not create
Kubernetes resources. It creates `job.Job` domain objects in
PostgreSQL. The Kubernetes `SyncJob` CR is a separate resource that
is created by users via `kubectl` or by the Console application.
The Console does not currently have a Kubernetes client and does
not create SyncJob CRs.

This creates an architectural gap: the observability contract
(`tenant_id` label required for per-tenant alerting) is documented
but not enforced at the data-plane boundary. Operators who create
SyncJobs without the label produce metrics with `_unknown`, which
prevents meaningful tenant-scoped SLO dashboards.

## Decision

### 1. Correct the CRD godoc

The `syncjob_types.go` SyncJob godoc is updated to remove the
incorrect "Labels are set by the API server at creation time"
statement. The correct statement is:

> The `astrasync.io/tenant-id` label MUST be set by the caller
> at SyncJob creation time. Users creating SyncJob resources via
> `kubectl` MUST include the label. Applications creating SyncJob
> resources programmatically MUST set the label from the
> authenticated principal's tenant context.

### 2. Add kubebuilder validation for the required label

> **Corrected by ADR-086.** Kubernetes CRD schemas cannot validate
> `metadata.labels`; the CEL rule below was not installable and has been
> removed. The label remains a required trusted-writer contract.

The `SyncJob` CRD is annotated with kubebuilder validation
markers that enforce the presence and format of the label:

```go
// +kubebuilder:validation:Required
// +kubebuilder:validation:Pattern=`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`
// +kubebuilder:validation:Description="Required. Canonical lowercase UUID of the tenant that owns this job. Used by controller_job_state_total and controller_epoch_fence_total for per-tenant observability (ADR-071)."
```

The pattern matches the canonical lowercase UUID format already
enforced by `observability/normalize.NormalizeTenant`. A label
value that does not match the pattern causes the Kubernetes API
server to reject the SyncJob CREATE or UPDATE with
`metav1.StatusReasonInvalid`. This is enforced at admission time
without any webhook or extension point.

### 3. CRD regeneration

`make crd-generate` is run to regenerate the CRD YAML manifests
in `deployment/operator/config/crd/bases/`. The generated CRD
contains the CEL validation rule for the label. No manual edits
to the CRD YAML are made.

### 4. Console injects the label at SyncJob creation

The Console (`console/`) is the primary user-facing surface for
creating SyncJob resources. The Console already derives the tenant
ID from the authenticated principal's session (`tenantIDForScope`
in `console/internal/authflow/manager.go`). The Console's REST
`createJob` handler (`console/internal/server/job_handlers.go`)
currently calls `s.mutations.CreateJob(...)`, which creates the
PostgreSQL `job.Job` record.

The Console gains a Kubernetes client (`client.Client` from
`controller-runtime`) and creates the corresponding SyncJob CR
after the PostgreSQL record is created. The SyncJob CR includes
`astrasync.io/tenant-id` from the session context. If the
Kubernetes client is not configured (e.g., in local dev mode),
the Console falls back to the PostgreSQL-only path and logs a
warning that the label was not set.

The Console does not update existing SyncJob resources that
lack the label (that is a future migration concern).

### 5. No controller change

The controller does not set the `astrasync.io/tenant-id` label.
It continues to read it from the SyncJob CR for metrics emission.
This preserves the ADR-066 §Key Design Decisions contract:
"the K8s label approach achieves the same observability goal with
a one-line controller change" — but that one-line change was
delayed until slice 49.1.5, and this ADR decides that the label
is enforced at the K8s API boundary rather than in the controller.

### 6. Backwards compatibility

Existing SyncJob resources that lack the `astrasync.io/tenant-id`
label will be **rejected** by the Kubernetes API server after the
CRD is updated and applied. This is a breaking change for existing
SyncJob resources. Operators must patch or recreate existing
SyncJob resources with the label before upgrading:

```bash
# Patch existing SyncJobs with the correct tenant ID
kubectl get syncjobs -A -o jsonpath='{range .items[*]}{.metadata.namespace}{"\t"}{.metadata.name}{"\n"}{end}' \
  | while read ns name; do
    kubectl label syncjob -n "$ns" "$name" astrasync.io/tenant-id="<tenant-uuid>" --overwrite
  done
```

This requirement is documented in the deployment documentation and
the ADR. The Phase 27 README includes a migration note.

## Consequences

### Positive

- `controller_job_state_total` and `controller_epoch_fence_total`
  emit real `tenant_id` values (not `_unknown`) for every SyncJob
  created after this phase, enabling tenant-scoped SLO dashboards.
- The kubebuilder validation prevents the `_unknown` gap from
  re-introducing silently. A SyncJob without the label cannot be
  created in any cluster where the CRD is installed.
- The Console's Kubernetes client path is the correct place for
  label injection: the Console has the authenticated principal's
  tenant context and creates both the PostgreSQL record and the
  K8s resource in the same logical operation.
- The `pattern` validation ensures the label value is a canonical
  lowercase UUID, matching the normalization contract already used
  by `observability/normalize.NormalizeTenant`.

### Negative

- **Breaking change for existing SyncJob resources.** Clusters that
  have existing SyncJobs without the label will fail admission after
  the CRD is updated. Operators must patch existing resources before
  upgrading. This is a one-time migration cost.
- The Console Kubernetes client is a new dependency. In local dev
  mode (without a K8s cluster), the Console falls back to the
  PostgreSQL-only path and logs a warning. This means local dev
  SyncJobs will not have the label and will produce `_unknown`
  metrics. This is acceptable for local development.
- The controller still reads the label from the SyncJob CR and
  emits `_unknown` for any resource that bypasses the Console
  (e.g., direct `kubectl` with incorrect labels). The kubebuilder
  validation prevents this at the API server level, but only for
  resources managed by the cluster's admission control.
- The `apiserver_session_revoke_total` metric still emits
  `_unknown` or `_platform` for principals without active
  memberships. This is a separate slice (future Phase 27+ work)
  that requires the API server to track tenant context per RPC.

## Alternatives Considered

### Controller sets the label when creating the `job.Job` from a SyncJob

The controller receives the SyncJob CR (which has the label after
this ADR's validation) and creates the `job.Job` in the repository.
The controller could record the tenant ID in the `job.Job` domain
model and expose it back for metrics emission.

**Reject.** This would require adding `TenantID string` to the
`job.Job` domain model, touching the repository interface, the
PostgreSQL schema, the memory repository, and every test that
constructs a `job.Job`. The K8s label approach achieves the same
observability goal without modifying the domain model.

### API Server creates the SyncJob CR

The API server could gain a Kubernetes client and create the SyncJob
CR directly after creating the `job.Job` in PostgreSQL.

**Reject.** The API server's core competency is the gRPC/REST
boundary and the PostgreSQL data plane. Adding a Kubernetes client
to the API server would couple the data-plane and control-plane
concerns. The Console is the correct place for the K8s write
because it is already the user-facing surface that manages
namespace-scoped resources.

### Webhook-based label injection (mutating admission webhook)

A mutating admission webhook could inject the `astrasync.io/tenant-id`
label based on the authenticated principal's context for every
SyncJob CREATE operation.

**Reject.** A webhook is heavyweight (requires a running webhook
server, TLS certificates, and cluster configuration) for a
one-line Console change. The Console's direct K8s client is
simpler and has lower operational overhead.

### Leave the label optional and fix via documentation only

Keep the label optional and document that users must set it.

**Reject.** This is the current state and it produces `_unknown`
metrics in practice. Operators forget to set labels. The ADR-066
§Consequences documents this limitation explicitly: "without it,
all `controller_epoch_fence_total` emissions carry `_unknown` as
the tenant label." The kubebuilder validation is the correct
enforcement mechanism that makes the contract self-enforcing.
