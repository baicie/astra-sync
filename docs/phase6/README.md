# Phase 6: Platform

Phase 6 turns the control plane into an operator-facing platform. Slice 17 establishes a usable
read-only Web Console. Slice 18 supplies authentication, tenant RBAC, and transactional audit.
Slice 19 adds versioned Job mutations, canonical validation, concurrency recovery, and operational
feedback. Slice 20 delivers the deployment-owned Connector Catalog and tenant Connection boundary
required to replace persisted raw credentials with auditable, epoch-fenced external Secret
references. Slice 18 through 20 are implemented; production enablement remains guarded by explicit
operator-controlled rollout gates.

## Roadmap

| Slice | Focus | Status |
|---|---|---|
| Slice 17 | Web Console and namespace-scoped read-only Job operations | Complete |
| Slice 18 | Authentication, tenant identity, RBAC, and audit policy | Slice 19/20 runtime foundation complete; admin rollout planned |
| Slice 19 | Job mutation workflows and operational actions | Implementation Complete; rollout gated |
| Slice 20 | Connector catalog and tenant Connection references | Implementation Complete; rollout gated |

## Records

- [Slice 17 README](17-web-console-job-readonly/README.md)
- [Slice 17 Design](17-web-console-job-readonly/design.md)
- [Slice 17 Implementation Plan](17-web-console-job-readonly/implementation-plan.md)
- [Slice 17 Verification](17-web-console-job-readonly/verification.md)
- [ADR-035: Namespace-scoped Read-only Job Console](../adr/adr-035-namespace-scoped-read-only-job-console.md)
- [Slice 18 README](18-auth-rbac-audit/README.md)
- [Slice 18 Design](18-auth-rbac-audit/design.md)
- [Slice 18 Threat Model](18-auth-rbac-audit/threat-model.md)
- [Slice 18 Authorization Matrix](18-auth-rbac-audit/authorization-matrix.md)
- [Slice 18 Implementation Plan](18-auth-rbac-audit/implementation-plan.md)
- [Slice 18 Verification](18-auth-rbac-audit/verification.md)
- [ADR-036: External OIDC and Local Tenant Authorization](../adr/adr-036-external-oidc-and-local-tenant-authorization.md)
- [ADR-037: Transactional Control-plane Audit Trail](../adr/adr-037-transactional-control-plane-audit-trail.md)
- [Slice 19 README](19-job-operations/README.md)
- [Slice 19 Design](19-job-operations/design.md)
- [Slice 19 Workflow Matrix](19-job-operations/workflow-matrix.md)
- [Slice 19 Validation and Secrets](19-job-operations/validation-and-secrets.md)
- [Slice 19 Implementation Plan](19-job-operations/implementation-plan.md)
- [Slice 19 Verification](19-job-operations/verification.md)
- [ADR-038: Desired-state Job Mutation Workflows](../adr/adr-038-desired-state-job-mutation-workflows.md)
- [ADR-039: Canonical Side-effect-free JobSpec Validation](../adr/adr-039-canonical-side-effect-free-jobspec-validation.md)
- [Slice 20 README](20-connector-catalog-connections/README.md)
- [Slice 20 Design](20-connector-catalog-connections/design.md)
- [Slice 20 Descriptor Contract](20-connector-catalog-connections/descriptor-contract.md)
- [Slice 20 Connection Lifecycle](20-connector-catalog-connections/connection-lifecycle.md)
- [Slice 20 Security and Materialization](20-connector-catalog-connections/security-and-materialization.md)
- [Slice 20 Authorization Matrix](20-connector-catalog-connections/authorization-matrix.md)
- [Slice 20 Implementation Plan](20-connector-catalog-connections/implementation-plan.md)
- [Slice 20 Migration and Rollback Runbook](20-connector-catalog-connections/migration-and-rollback.md)
- [Slice 20 Verification](20-connector-catalog-connections/verification.md)
- [ADR-040: Deployment-authoritative Connector Descriptor Catalog](../adr/adr-040-deployment-authoritative-connector-catalog.md)
- [ADR-041: External Secret References and Epoch-scoped Credential Materialization](../adr/adr-041-external-secrets-epoch-credential-materialization.md)

## Boundary

Slice 17 provides inspection only. The Console fixes its namespace at startup and delegates all
job reads to the existing control-plane `JobService`. It does not authenticate users, select a
tenant from browser input, or expose lifecycle mutation methods.

Slice 18's Slice 19/20 runtime foundation keeps identity proof at an external OIDC provider, resolves tenant roles from local
PostgreSQL state, enforces permissions independently in the API Server, and records
security-relevant actions in an append-only transactional audit trail. The Console uses opaque
server-side sessions and same-origin CSRF protection. Standalone identity/membership/audit
administration APIs and production IdP rollout remain tracked by the Slice 18 implementation plan;
production also requires deployment-owned TLS, key, membership, and retention configuration.

Slice 19 reuses the durable desired-state Job lifecycle, requires expected versions and
idempotency for operator writes, distinguishes accepted commands from asynchronous convergence,
and puts the Java JobCompiler behind a side-effect-free canonical validation boundary. Console
mutation workflows are delivered behind the authenticated BFF and API authorization boundary.

Slice 20 keeps the deployed Java connector inventory as executable authority and persists immutable
catalog snapshots plus tenant Connection metadata in PostgreSQL. Credential bytes remain in
external immutable Secrets. Start captures stable Connection generations per epoch before
Scheduler materialization, and runtime bootstrap consumes only the strict mounted credential
envelope. Connection mutations, testing, and runtime admission default to disabled; operators use
the migration and rollback runbook to enable each stage without claiming that a repository merge
is a production rollout.
