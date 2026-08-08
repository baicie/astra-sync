# Phase 6 Slice 20: Connector Catalog and Tenant Connection References

## Status

Design complete; implementation not started.

Slice 20 defines the deployment-owned Connector Catalog and the tenant-scoped Connection resource
needed to replace raw credentials in persisted JobSpecs. It closes the design gap left by ADR-030
and Slice 19 without claiming that `connection_ref` can be dispatched by the current runtime.

## Design Outcomes

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
- A staged implementation and migration plan that preserves the Scheduler's fail-closed behavior
  until end-to-end materialization is verified.

## Records

- [Design](design.md)
- [Descriptor contract](descriptor-contract.md)
- [Connection lifecycle](connection-lifecycle.md)
- [Security and materialization](security-and-materialization.md)
- [Authorization matrix](authorization-matrix.md)
- [Implementation plan](implementation-plan.md)
- [Design verification](verification.md)
- [ADR-040: Deployment-authoritative Connector Descriptor Catalog](../../adr/adr-040-deployment-authoritative-connector-catalog.md)
- [ADR-041: External Secret References and Epoch-scoped Credential Materialization](../../adr/adr-041-external-secrets-epoch-credential-materialization.md)

## Implementation Gate

The current Scheduler must continue rejecting every non-empty `connectionRef`. That rejection can
be removed only after the catalog, Connection repository, tenant authorization, epoch binding,
Secret provider, runtime injection, redaction, cleanup, and failure-path tests are implemented as
one guarded path. The legacy unowned Connection CRD is not evidence that any of those capabilities
exist.
