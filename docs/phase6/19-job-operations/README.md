# Phase 6 Slice 19: Job Mutation Workflows and Operational Actions

## Status

Implementation complete; production mutation rollout remains operator-controlled.

Slice 19 implements how authenticated operators create, validate, edit, start, stop, restart, and
delete Jobs without weakening the lifecycle, tenant, concurrency, or audit boundaries established
by earlier slices. `JobService` remains the lifecycle authority; the Console adds a same-origin
workflow layer rather than another Job model.

## Design Outcomes

- One permission- and state-aware workflow for every existing `JobService` RPC.
- Positive expected versions on Console writes and explicit recovery from optimistic conflicts.
- Idempotency keys for all externally retried mutations, correlated with transactional audit.
- Asynchronous Start and Stop feedback based on desired-state acceptance and status convergence.
- `StartJob` reused as Restart for terminal executions, preserving epoch fencing.
- A protobuf-first, side-effect-free canonical JobSpec validation boundary backed by the Java
  `JobCompiler` and connector descriptors.
- Descriptor-driven forms, pre-provisioned `connection_ref` values, and strict secret redaction.
- Guarded rollout that cannot expose Console writes before Slice 18 authentication, RBAC, CSRF,
  TLS, and transactional audit are implemented.

## Records

- [Design](design.md)
- [Workflow matrix](workflow-matrix.md)
- [Validation and secrets](validation-and-secrets.md)
- [Implementation plan](implementation-plan.md)
- [Design verification](verification.md)
- [ADR-038: Desired-state Job Mutation Workflows](../../adr/adr-038-desired-state-job-mutation-workflows.md)
- [ADR-039: Canonical Side-effect-free JobSpec Validation](../../adr/adr-039-canonical-side-effect-free-jobspec-validation.md)

## Rollout Gate

The Console mutation routes are implemented behind the Slice 18 BFF, authorization, CSRF,
idempotency, optimistic concurrency, canonical validation, and transactional audit boundaries.
Production exposure remains conditional on deployment OIDC/TLS configuration and the Slice 20
Connection rollout gates. Validation is never treated as authorization or as evidence that a later
mutation is safe to skip.
