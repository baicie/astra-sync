# Phase 6 Slice 20: Connector Catalog and Tenant Connection References

## Status

Implementation complete; production rollout is operator-controlled and disabled by default.

Slice 20 defines the deployment-owned Connector Catalog and the tenant-scoped Connection resource
needed to replace raw credentials in persisted JobSpecs. The implementation closes the design gap
left by ADR-030 and Slice 19 while keeping all write, test, and runtime capabilities behind
fail-closed rollout gates.

## Delivered Outcomes

- A read-only Connector Catalog derived from the exact connector artifacts deployed with the Java
  compiler and runtime, with stable descriptor and inventory revisions.
- A versioned descriptor contract for forms, canonical validation, connection compatibility, and
  runtime connector fencing; tenants cannot upload connector binaries or descriptors.
- A PostgreSQL-authoritative, tenant-scoped Connection resource with optimistic versions, stable
  UIDs, immutable credential generations, and no secret bytes in control-plane storage.
- Explicit permissions, transactional audit, idempotency, cross-tenant denial, lifecycle rules,
  and reference-safe deletion.
- An external Secret provider boundary, beginning with immutable Kubernetes Secrets, plus
  execution-epoch credential materialization and deterministic cleanup.
- A separately permissioned, rate-limited, isolated, read-only connection test operation that is
  never part of canonical JobSpec validation.
- A staged migration and rollback runbook with API and Scheduler gates disabled by default.

## Records

- [Design](design.md)
- [Descriptor contract](descriptor-contract.md)
- [Connection lifecycle](connection-lifecycle.md)
- [Security and materialization](security-and-materialization.md)
- [Authorization matrix](authorization-matrix.md)
- [Implementation plan](implementation-plan.md)
- [Migration and rollback runbook](migration-and-rollback.md)
- [Operator enablement](enablement.md)
- [Verification](verification.md)
- [ADR-040: Deployment-authoritative Connector Descriptor Catalog](../../adr/adr-040-deployment-authoritative-connector-catalog.md)
- [ADR-041: External Secret References and Epoch-scoped Credential Materialization](../../adr/adr-041-external-secrets-epoch-credential-materialization.md)

## Rollout Gate

`CONNECTION_MUTATIONS_ENABLED`, `CONNECTION_TESTS_ENABLED`, `CONNECTION_RUNTIME_ENABLED`, and
`SCHEDULER_CONNECTION_MATERIALIZATION_ENABLED` default to `false`. A disabled runtime gate rejects
Start for Jobs with `connection_ref`; Catalog and Connection reads remain available. Helm prevents
runtime admission without Scheduler materialization and prevents test admission without the
isolated executor. The legacy unowned Connection CRD is deprecated and is not a supported API or
migration source.
