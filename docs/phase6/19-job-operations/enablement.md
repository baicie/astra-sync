# Phase 6 Slice 19 Operator Enablement

## Purpose

This document explains how an operator enables Console Job mutations and the
canonical-validation admission path in a Slice 19 deployment. Slice 19 is implemented
behind a feature gate; the gate cannot be enabled until every Slice 18 prerequisite is in
place. This doc captures the gate key, the prerequisites, the staging rollout steps, the
production enablement steps, and the rollback procedure.

The authoritative state is recorded in the
[Slice 19 design](design.md), the [Slice 19 implementation plan](implementation-plan.md),
and the [Slice 19 verification](verification.md). The [Workflow matrix](workflow-matrix.md)
documents the action-permission-version expectations and the
[Validation and secrets](validation-and-secrets.md) document captures the canonical
validation boundary.

## Feature Gate

| Environment variable | Default | Production rule |
|---|---|---|
| `CONSOLE_JOB_MUTATIONS_ENABLED` | `false` | Refused unless every prerequisite listed below is enabled |

The flag controls both route registration in the Console BFF and UI rendering. Disabling
the flag hides the Create, Edit, Start, Restart, Stop, and Delete controls on the Job
detail page and returns `404` from the mutation routes.

## Prerequisites

Before enabling `CONSOLE_JOB_MUTATIONS_ENABLED` in any environment, the operator confirms:

1. Slice 18 OIDC validation is enabled and the IdP issuer, audience, and signing keys are
   reachable from the control plane. JWTs without a registered kid, expired tokens, or
   tokens outside the lifetime / audience bounds are rejected by the API Server.
2. Slice 18 tenant membership, role resolution, and the deny-by-default method registry
   are deployed. The `authn.Registry.ValidateServices` startup check passes on every
   control-plane replica.
3. Slice 18 transactional audit is writing to `astrasync_security_audit_events` and the
   API database role cannot `UPDATE` or `DELETE` audit rows.
4. Console BFF session, CSRF, opaque cookie, idle/absolute expiry, and session revocation
   are deployed. PKCE state, nonce, and replay protection are verified end-to-end.
5. Production TLS, trusted-proxy validation, secret references, and fail-closed startup
   checks are all enabled.
6. The Java compiler-validation service is deployed alongside the Go API. Its gRPC
   endpoint is reachable only from the API Server network and uses the same release
   compatibility check as the Coordinator dispatch path.
7. The Connector Catalog and Connection rollout gates from Slice 20 are aligned with the
   Slice 19 rollout scope. If Connections are not yet enabled, Jobs using
   `connection_ref` remain blocked at Start and the operator documents this in the
   staging acceptance memo.

If any prerequisite is missing, the control plane refuses to enable
`CONSOLE_JOB_MUTATIONS_ENABLED` and emits a structured startup error. The operator
consults the [Slice 18 admin runbook](../18-auth-rbac-audit/admin-runbook.md) and the
[Slice 20 migration and rollback runbook](../20-connector-catalog-connections/migration-and-rollback.md)
for recovery.

## Staging Rollout Steps

1. Deploy Slice 18 prerequisites to staging and run the full CI suite (`make ci` or the
   equivalent pipeline).
2. Enable `CONSOLE_JOB_MUTATIONS_ENABLED=false` initially. Run shadow validation
   against existing Jobs to surface any compiler or descriptor incompatibilities. The
   validation results go to the audit log with outcome `NO_CHANGE`.
3. Block any tenant whose JobSpecs reference deprecated sensitive options or connector
   configurations that no longer compile. The block is enforced by the validation result
   the next time the operator opens the editor.
4. Enable `CONSOLE_JOB_MUTATIONS_ENABLED=true` for staging. Authenticate as a
   `tenant_admin` and exercise every workflow matrix cell in the
   [Workflow matrix](workflow-matrix.md), verifying the audit outcome, idempotency replay,
   and convergence polling behavior.
5. Reconcile Job / idempotency / audit counts against the pre-rollout snapshot. Any
   discrepancy blocks the production enablement.
6. Exercise the rollback procedure in staging before requesting production enablement.

## Production Enablement

1. Complete the staging acceptance memo. The memo must reference the staging tenant
   identifier, the verification date, the operators who signed off, and the reconciliations
   that passed.
2. Schedule the production enablement with the change review board. The review cites
   this doc, the staging memo, and the [Slice 19 verification](verification.md).
3. Enable `CONSOLE_JOB_MUTATIONS_ENABLED=true` on one production tenant first. The
   chosen tenant must already be reachable through the Slice 18 OIDC flow and have at
   least one tenant administrator.
4. Observe the new tenant for the documented acceptance window. The acceptance signals
   are:
   - mutation latency (Create / Update / Start / Stop / Delete P95 within the SLO),
   - conflict and no-op rate (both non-zero is expected; a sudden jump means a stale
     Console or a bad descriptor),
   - compiler error rate (must remain below the staging baseline),
   - audit failure rate (must remain at zero; any failure triggers immediate rollback),
   - convergence timeout rate (jobs that never reach the desired state within the SLO
     are investigated before expanding).
5. Expand to additional tenants one at a time. Each tenant has the same acceptance
   window. The expansion is gated on the previous tenant's signals.
6. After all tenants are enabled, mark `CONSOLE_JOB_MUTATIONS_ENABLED=true` in the
   configuration baseline. Remove the staging-only annotation.

## Rollback Procedure

Rollback disables new Console writes while leaving the desired state intact.

1. Set `CONSOLE_JOB_MUTATIONS_ENABLED=false` on the affected replicas or tenants.
   Existing accepted desired states continue to converge.
2. The Console hides the mutation controls and the BFF returns `404` from the
   mutation routes. Read routes (`GetJob`, `ListJobs`, `GetJobStatus`,
   `ValidateJobSpec`) remain reachable.
3. Operators use the authenticated API to recover any in-flight mutations that need a
   new desired state. The API keeps the expected-version and idempotency-key
   requirements so retries stay safe.
4. Investigate the rollback trigger. Common triggers include elevated audit-write
   failure rate, elevated conflict rate that the Console cannot recover from, or a
   discovered regression in the compiler-validation service.
5. After the trigger is fixed, repeat the staging rollout before re-enabling the gate
   in production.

Rollback never switches the API to anonymous access and never silently reverses a
Start, Stop, Update, or Delete. Rollback only closes the new-mutation path.

## Observability

Operators monitor the Slice 19 enablement using the same dashboards as Slice 18.
Critical signals:

| Signal | Source | Threshold |
|---|---|---|
| `mutation.accepted` counter | audit log filtered by `job.create` / `job.update` / `job.start` / `job.stop` / `job.delete` | steady growth during rollout |
| `mutation.denied` counter | audit log filtered by the same event types and outcome `DENIED` | spike indicates permission or scope misconfiguration |
| `mutation.audit_failed` counter | audit log filtered by outcome `AUDIT_WRITE_FAILED` | must remain at zero |
| `validation.issue` counter | audit log filtered by `job.validate` | spike indicates connector or descriptor drift |
| `idempotency.replay` counter | audit log filtered by `idempotency.replayed` | confirms retries are deduped |

The acceptance memo records the baseline values for each tenant. Any deviation during
production enablement triggers the rollback procedure.

## Records

- [Slice 19 README](README.md)
- [Slice 19 Design](design.md)
- [Slice 19 Implementation Plan](implementation-plan.md)
- [Slice 19 Verification](verification.md)
- [Slice 19 Workflow Matrix](workflow-matrix.md)
- [Slice 19 Validation and Secrets](validation-and-secrets.md)
- [Slice 18 admin runbook](../18-auth-rbac-audit/admin-runbook.md)
- [Slice 20 migration and rollback runbook](../20-connector-catalog-connections/migration-and-rollback.md)
- [ADR-038: Desired-state Job Mutation Workflows](../../adr/adr-038-desired-state-job-mutation-workflows.md)
- [ADR-039: Canonical Side-effect-free JobSpec Validation](../../adr/adr-039-canonical-side-effect-free-jobspec-validation.md)
