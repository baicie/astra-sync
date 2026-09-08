# Phase 29 — API Server Consumes `x-astra-tenant-id`

Phase 29 closes the **server-side consumption** half of the tenant-id
envelope contract that Phase 28 (ADR-071 / ADR-072 / ADR-073) opened on
the Kubernetes admission boundary, the Console BFF egress boundary, and
the SyncJob CR creation path. The full chain is now:

1. **Egress** (ADR-072, Phase 28-A, shipped): the Console BFF appends
   `x-astra-tenant-id` to outgoing gRPC metadata on every job mutation.
2. **Admission** (ADR-071, Phase 27, shipped): every `SyncJob` CR
   carries the `astrasync.io/tenant-id` kubebuilder-validated label.
3. **Creation** (ADR-073, Phase 28-B, Proposed): the Console creates the
   `SyncJob` CR with the label after the durable PostgreSQL write.
4. **Consumption** (ADR-074, Phase 29, **this README**): the API Server
   extracts `x-astra-tenant-id` from incoming metadata, reconciles it
   against the principal's active membership, and writes the verified
   tenant-id into the `job.Job.tenant_id` PostgreSQL column.

## What ships in Phase 29

- `control-plane/api-server/internal/authn/tenant_metadata.go` —
  the canonical-UUID extractor and the `WithJobTenantID` /
  `JobTenantIDFromContext` context helpers.
- `control-plane/api-server/internal/authn/interceptor.go` —
  `reconcileTenantMetadata` is the new trust boundary. It runs after
  the existing membership resolution and before the Authorizer; a
  mismatch between the metadata and the membership's `TenantID`
  rejects the request with `PermissionDenied` and audits
  `TENANT_DENIED`. Malformed metadata rejects with
  `TENANT_ENVELOPE_INVALID`.
- `control-plane/api-server/internal/service/job_validation_service.go` —
  `resolvedTenantIDForMutation` is the JobService consumer. It reads
  the verified tenant-id from the request context, falling back to the
  membership-derived value when no interceptor ran (legacy callers,
  direct unit tests).
- `control-plane/api-server/internal/service/job_mutation_service.go` —
  `newJobMutation` calls `resolvedTenantIDForMutation` instead of
  `tenantIDForConnectionUse` directly. The audit row written by the
  mutation repository now carries the same verified tenant-id the
  interceptor authorised the request for.
- `control-plane/job/postgres/migrations/003_jobs_tenant_id.sql` —
  adds the nullable `tenant_id UUID` column to
  `astrasync_control_jobs`, with an index on the column.
- `control-plane/job/postgres/repository.go` — `Migrate()` now
  applies `003_jobs_tenant_id.sql`. The legacy `Repository.Create`
  path writes `NULL` (only the lifecycle controller reaches it; the
  tenant binding flows through the mutation repository path).
- `control-plane/job/postgres/mutation_repository.go` —
  `createJobMutation` writes `mutation.TenantID` into the
  `tenant_id` column on INSERT. `updateLockedJob` does not need to
  SET `tenant_id`; the column is preserved by the `WHERE uid` clause.

## Test coverage

- `control-plane/api-server/internal/authn/tenant_metadata_test.go` —
  5 cases (absent, canonical, duplicate, non-UUID, context round-trip).
- `control-plane/api-server/internal/authn/interceptor_test.go` —
  3 new cases (`TestInterceptorAttachesVerifiedTenantID`,
  `TestInterceptorRejectsTenantMetadataMismatch`,
  `TestInterceptorRejectsMalformedTenantMetadata`).
- `control-plane/api-server/internal/service/job_mutation_service_test.go` —
  2 new cases
  (`TestTransactionalJobCreatePrefersAttachedTenantID`,
  `TestTransactionalJobCreateFallsBackToMembershipWhenNoAttachedTenantID`).
- All existing tests continue to pass (`go test ./... -count=1`).

## Deferred work (follow-up ADR)

- **Backfill `tenant_id` for pre-Phase-29 rows.** The new column is
  nullable to avoid breaking existing rows. A future migration should
  `UPDATE astrasync_control_jobs SET tenant_id = <lookup>` by joining
  against `astrasync_auth_tenants` on `namespace`, and tighten the
  constraint to `NOT NULL` once the backfill completes.
- **Tighten `Repository.Create` / `Repository.Update` to write
  `tenant_id`.** Today those paths leave the column NULL because the
  lifecycle controller's direct calls do not carry a tenant. After
  the backfill and the lifecycle-controller audit, the contract can
  tighten so every write carries an explicit tenant-id.
- **Expose `tenant_id` in the public `job.Job` struct.** Today the
  binding is captured only at the SQL boundary (see ADR-074 §5). A
  future slice may add it to the read model for tenant-scoped job
  list endpoints.

## Metrics impact

The `controller_job_state_total{tenant_id="..."}` and
`controller_epoch_fence_total{tenant_id="..."}` metrics now see
real tenant-ids (not `_unknown`) for every Console-issued job once
Slice 28-B + Phase 29 are both in production:

- Slice 28-B creates the `SyncJob` CR with the
  `astrasync.io/tenant-id` label.
- Phase 29 ensures the API Server-side audit + mutation rows are
  written under the same tenant-id the BFF declared.
- The controller reads the label and joins against
  `astrasync_auth_tenants` to emit the metric.

Until Slice 28-B lands in production, Console-issued jobs still emit
`_unknown` because no `SyncJob` CR is created (the metric is emitted
at reconcile time, not at create time). Phase 29 is the prerequisite
for the metric becoming real once Slice 28-B follows.