# Phase 6: Platform

Phase 6 turns the control plane into an operator-facing platform. Slice 17 establishes a usable
read-only Web Console. Slice 18 supplies the authentication, tenant RBAC, and audit design required
before writes are exposed. Slice 19 now defines implementation-ready Job mutation, canonical
validation, concurrency, and operational feedback workflows. Slice 20 defines the deployment-owned
Connector Catalog and tenant Connection boundary required to replace persisted raw credentials with
auditable, epoch-fenced external Secret references.

## Roadmap

| Slice | Focus | Status |
|---|---|---|
| Slice 17 | Web Console and namespace-scoped read-only Job operations | Complete |
| Slice 18 | Authentication, tenant identity, RBAC, and audit policy | Design Complete |
| Slice 19 | Job mutation workflows and operational actions | Design Complete |
| Slice 20 | Connector catalog and tenant Connection references | Design Complete |

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
- [Slice 18 Design Verification](18-auth-rbac-audit/verification.md)
- [ADR-036: External OIDC and Local Tenant Authorization](../adr/adr-036-external-oidc-and-local-tenant-authorization.md)
- [ADR-037: Transactional Control-plane Audit Trail](../adr/adr-037-transactional-control-plane-audit-trail.md)
- [Slice 19 README](19-job-operations/README.md)
- [Slice 19 Design](19-job-operations/design.md)
- [Slice 19 Workflow Matrix](19-job-operations/workflow-matrix.md)
- [Slice 19 Validation and Secrets](19-job-operations/validation-and-secrets.md)
- [Slice 19 Implementation Plan](19-job-operations/implementation-plan.md)
- [Slice 19 Design Verification](19-job-operations/verification.md)
- [ADR-038: Desired-state Job Mutation Workflows](../adr/adr-038-desired-state-job-mutation-workflows.md)
- [ADR-039: Canonical Side-effect-free JobSpec Validation](../adr/adr-039-canonical-side-effect-free-jobspec-validation.md)
- [Slice 20 README](20-connector-catalog-connections/README.md)
- [Slice 20 Design](20-connector-catalog-connections/design.md)
- [Slice 20 Descriptor Contract](20-connector-catalog-connections/descriptor-contract.md)
- [Slice 20 Connection Lifecycle](20-connector-catalog-connections/connection-lifecycle.md)
- [Slice 20 Security and Materialization](20-connector-catalog-connections/security-and-materialization.md)
- [Slice 20 Authorization Matrix](20-connector-catalog-connections/authorization-matrix.md)
- [Slice 20 Implementation Plan](20-connector-catalog-connections/implementation-plan.md)
- [Slice 20 Design Verification](20-connector-catalog-connections/verification.md)
- [ADR-040: Deployment-authoritative Connector Descriptor Catalog](../adr/adr-040-deployment-authoritative-connector-catalog.md)
- [ADR-041: External Secret References and Epoch-scoped Credential Materialization](../adr/adr-041-external-secrets-epoch-credential-materialization.md)

## Boundary

Slice 17 provides inspection only. The Console fixes its namespace at startup and delegates all
job reads to the existing control-plane `JobService`. It does not authenticate users, select a
tenant from browser input, or expose lifecycle mutation methods.

Slice 18 now has an implementation-ready design. It keeps identity proof at an external OIDC
provider, resolves tenant roles from local PostgreSQL state, enforces permissions independently in
the API Server, and records security-relevant actions in an append-only audit trail. No Slice 18
runtime capability is claimed until its implementation and verification gates pass.

Slice 19 also remains design-only. It reuses the durable desired-state Job lifecycle, requires
expected versions and idempotency for operator writes, distinguishes accepted commands from
asynchronous convergence, and puts the Java JobCompiler behind a side-effect-free canonical
validation boundary. Console mutation routes remain disabled until the Slice 18 runtime and Slice
19 mutation/audit gates are implemented and verified.

Slice 20 is also design-only. The deployed Java connector inventory remains the executable
authority; PostgreSQL would store immutable catalog snapshots and tenant Connection metadata, while
credential bytes remain in external immutable Secrets. A changed Start would capture a stable
Connection generation for its epoch before Scheduler materialization. The existing Scheduler must
continue rejecting non-empty `connectionRef` values until authorization, redaction, provider,
runtime injection, race, and cleanup gates are implemented and verified end to end.
