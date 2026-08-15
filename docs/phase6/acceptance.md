# Phase 6 Acceptance Document

## Purpose

This document is the cross-slice acceptance reference for Phase 6 (Platform). It collects
the slice-level verification evidence into one index so an operator or auditor can confirm
that the Phase 6 implementation matches its design and that production enablement is
governed by explicit gates. It does not replace the slice-level documents — those remain
authoritative.

## Scope

Phase 6 covers Slices 17 through 21:

| Slice | Focus | Verification record |
|---|---|---|
| Slice 17 | Read-only Web Console and namespace-scoped Job operations | [Slice 17 verification](17-web-console-job-readonly/verification.md) |
| Slice 18 | Authentication, tenant identity, RBAC, audit policy, and tenant / role administration | [Slice 18 verification](18-auth-rbac-audit/verification.md) |
| Slice 19 | Job mutation workflows and operational actions | [Slice 19 verification](19-job-operations/verification.md) |
| Slice 20 | Connector catalog and tenant Connection references | [Slice 20 verification](20-connector-catalog-connections/verification.md) |
| Slice 21 | Tenant audit explorer | [Slice 21 verification](21-audit-explorer/verification.md) |

Phase 6 is accepted at the repository level when every slice above carries its own
verification status. Production enablement is a separate operator-controlled step
recorded in the per-slice enablement documents.

## Repository Acceptance

### Design Gate

The Phase 6 design gate is documented in [Phase 6 README](README.md). Each slice's
implementation plan restates the relevant subset of the gate and references the design
documents that satisfy it.

### Functional Coverage

The repository must show evidence for:

- [x] OIDC discovery and JWKS validation with bounded algorithm, audience, lifetime, and
      dimension limits.
- [x] Tenant membership and role resolution from PostgreSQL with last-admin protection.
- [x] Console BFF login, callback, logout, and tenant-list routes behind PKCE state and
      nonce validation, opaque session cookies, and same-origin CSRF.
- [x] Deny-by-default gRPC authorization registry covering every public method across
      `JobService`, `JobValidationService`, `ConnectorCatalogService`,
      `ConnectionService`, `AuditService`, `IdentityService`, and `AccessService`.
- [x] Cross-tenant authorization, existence-oracle, role-revocation, and unknown-method
      tests in the authn interceptor suite.
- [x] Append-only transactional audit persistence for membership / platform role
      mutations (Slice 18.5), Job mutations (Slice 19), Connection lifecycle (Slice 20),
      and audit reads (Slice 21).
- [x] Bounded audit-event query with HMAC continuation tokens and synchronous read
      auditing (Slice 21).
- [x] Canonical validation boundary backed by the Java `JobCompiler` with descriptor
      ownership and side-effect-free validation (Slice 19).
- [x] Connector inventory publication, descriptor ownership, and CAS catalog activation
      (Slice 20).
- [x] External Secret provider boundary and Scheduler materialization with immutable
      epoch credentials and deterministic cleanup (Slice 20).
- [x] Isolated connection test executor with read-only probes, bounded admission, and
      FencingClaim lease semantics (Slice 20).

### Static Verification Procedure

Operators run the following checks before accepting a Phase 6 release:

1. Resolve every relative Markdown link in the Phase 6 directory, the ADR index, and the
   architecture baseline.
2. Confirm `git diff --check` is clean on the release branch.
3. Run `make ci` (or the equivalent pipeline) including:
   - Go unit tests in `control-plane/api-server`, `control-plane/auth`,
     `control-plane/scheduler`, and `control-plane/coordinator`;
   - PostgreSQL integration tests in `control-plane/auth/postgres`,
     `control-plane/scheduler/postgres`, and any other repository package;
   - Protocol determinism tests for the connector catalog;
   - Helm render matrix for default, invalid, and full configurations;
   - Container build for the API, Console, Coordinator, Worker, and Scheduler images;
   - Browser end-to-end tests for the Console workflows.
4. Compare the deployment connector inventory across compiler, API, Coordinator, and
   Worker images with `make catalog-check`.

A failure on any of the four steps blocks the Phase 6 acceptance.

## Operator Acceptance

Operator acceptance is gated by the per-slice enablement documents. Each gate has its
own prerequisite list, staging rollout steps, production rollout steps, rollback
procedure, and observability signals.

| Slice | Operator enablement | Default state |
|---|---|---|
| Slice 17 | Always on once deployed (read-only Console). | enabled |
| Slice 18 | Always on for authentication; Console session boundary ships behind the same gate as Slice 19. | enabled |
| Slice 19 | `CONSOLE_JOB_MUTATIONS_ENABLED` | `false` |
| Slice 20 | `CONNECTION_MUTATIONS_ENABLED`, `CONNECTION_TESTS_ENABLED`, `CONNECTION_RUNTIME_ENABLED`, `SCHEDULER_CONNECTION_MATERIALIZATION_ENABLED`, `connectionTestExecutor.enabled` | `false` |
| Slice 21 | Always on once Slice 18 is enabled (audit reads inherit the Slice 18 boundary). | enabled |

The enablement documents are:

- [Slice 18 admin runbook](18-auth-rbac-audit/admin-runbook.md)
- [Slice 19 operator enablement](19-job-operations/enablement.md)
- [Slice 20 operator enablement](20-connector-catalog-connections/enablement.md)

## Boundary Notes

The Phase 6 acceptance does not claim:

- Interoperability with a deployment's chosen identity provider. The repository tests
  cover the validator, the resolver, and the session boundary using deterministic
  test fixtures.
- A completed production rollout. Every runtime-affecting gate defaults closed and
  operators must explicitly enable each stage.
- Tenant and membership administration outside the implemented
  `IdentityService` / `AccessService` surface. The CLI utility
  `astra-auth-admin` provides the offline bootstrap and recovery operations.
- Platform-wide audit search, export, retention mutation, alerting, or any other
  read-write surface beyond what the slice design documents explicitly cover.
- A change to the existing lifecycle authority. `JobService` remains the lifecycle
  authority and the Console is a workflow layer on top of it.

## Records

- [Phase 6 README](README.md)
- [Slice 17 README](17-web-console-job-readonly/README.md)
- [Slice 17 Verification](17-web-console-job-readonly/verification.md)
- [Slice 18 README](18-auth-rbac-audit/README.md)
- [Slice 18 Verification](18-auth-rbac-audit/verification.md)
- [Slice 18 Transactional Audit Unit](18-auth-rbac-audit/transactional-audit.md)
- [Slice 19 README](19-job-operations/README.md)
- [Slice 19 Verification](19-job-operations/verification.md)
- [Slice 19 Operator Enablement](19-job-operations/enablement.md)
- [Slice 20 README](20-connector-catalog-connections/README.md)
- [Slice 20 Verification](20-connector-catalog-connections/verification.md)
- [Slice 20 Operator Enablement](20-connector-catalog-connections/enablement.md)
- [Slice 21 README](21-audit-explorer/README.md)
- [Slice 21 Verification](21-audit-explorer/verification.md)

## ADRs

- [ADR-035: Namespace-scoped Read-only Job Console](../adr/adr-035-namespace-scoped-read-only-job-console.md)
- [ADR-036: External OIDC and Local Tenant Authorization](../adr/adr-036-external-oidc-and-local-tenant-authorization.md)
- [ADR-037: Transactional Control-plane Audit Trail](../adr/adr-037-transactional-control-plane-audit-trail.md)
- [ADR-038: Desired-state Job Mutation Workflows](../adr/adr-038-desired-state-job-mutation-workflows.md)
- [ADR-039: Canonical Side-effect-free JobSpec Validation](../adr/adr-039-canonical-side-effect-free-jobspec-validation.md)
- [ADR-040: Deployment-authoritative Connector Descriptor Catalog](../adr/adr-040-deployment-authoritative-connector-catalog.md)
- [ADR-041: External Secret References and Epoch-scoped Credential Materialization](../adr/adr-041-external-secrets-epoch-credential-materialization.md)
- [ADR-042: Tenant-scoped Audited Security Event Queries](../adr/adr-042-tenant-scoped-audited-security-event-queries.md)
