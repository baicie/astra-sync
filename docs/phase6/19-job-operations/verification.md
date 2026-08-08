# Phase 6 Slice 19 Design Verification

## Status

Design complete; runtime verification awaits implementation.

## Design Checks

| Check | Result |
|---|---|
| Existing Job lifecycle and all eight JobService RPCs inventoried | PASS |
| Create, Edit, Start/Restart, Stop, Delete, and read workflows specified | PASS |
| Observed-state action availability and desired-state convergence specified | PASS |
| Expected-version conflict and explicit draft recovery specified | PASS |
| Idempotency scope, digest mismatch, replay, tombstone, and audit correlation specified | PASS |
| Command acceptance, polling, timeout, and unknown-outcome semantics separated | PASS |
| Canonical validation ownership follows Java JobCompiler and connector descriptors | PASS |
| Validation is bounded, side-effect free, revisioned, and enforced at mutation time | PASS |
| Permissions, tenant scope, CSRF, dangerous actions, and transactional audit specified | PASS |
| Raw-secret rejection, connection references, legacy migration, and redaction specified | PASS |
| Feature gate, prerequisites, rollout, rollback, and delivery order specified | PASS |
| Runtime capability claims excluded from Design Complete status | PASS |
| Local links, eight-RPC coverage, seven-state coverage, and staged whitespace validation | PASS |

## RPC Traceability

| RPC | Workflow evidence | Authorization evidence | Concurrency / recovery evidence |
|---|---|---|---|
| `CreateJob` | Design Create Workflow | `jobs.create` in JobService Coverage | Canonical validation and idempotent replay |
| `GetJob` | Console Information Architecture | `jobs.read` in JobService Coverage | Fresh load before edit and conflict refresh |
| `ListJobs` | Existing list entry point | `jobs.read` in JobService Coverage | Bounded tenant page |
| `UpdateJob` | Design Edit Workflow | `jobs.update` in JobService Coverage | Expected version and reload/reapply |
| `DeleteJob` | Design Delete Workflow | `jobs.delete` in JobService Coverage | Expected version and retry tombstone |
| `StartJob` | Design Start and Restart Workflow | `jobs.start` in JobService Coverage | Desired-state no-op, epoch, polling, same-key retry |
| `StopJob` | Design Stop Workflow | `jobs.stop` in JobService Coverage | Desired-state no-op, polling, same-key retry |
| `GetJobStatus` | Common Mutation State Machine | `jobs.read` in JobService Coverage | Polling plus full-Job version refresh before writes |

## Requirement Traceability

| Requirement | Design evidence |
|---|---|
| Preserve one lifecycle authority | Design dependencies and ADR-038 |
| Do not confuse acceptance with convergence | Common state machine and Start/Stop workflows |
| Do not silently overwrite concurrent edits | Optimistic concurrency section and workflow matrix |
| Recover ambiguous network outcomes | Idempotency section and failure/recovery matrix |
| Validate what runtime can compile | Validation ownership and ADR-039 |
| Keep validation free of external side effects | Validation mutation-time rules and security verification |
| Protect credentials | Connection References and Redaction Rules |
| Enforce tenant RBAC independently of UI | Authorization and Audit plus Slice 18 matrix |
| Couple mutations with durable evidence | Mutation unit of work and ADR-038 |
| Prevent premature exposure | Feature Gate and prerequisite implementation gate |

## Static Verification Procedure

Before merging the design change:

1. Resolve every relative Markdown link in the Slice 19 directory, Phase 6 index, ADR index, and
   architecture baseline.
2. Confirm the workflow matrix names all eight current `JobService` RPCs exactly once in its
   coverage table.
3. Confirm all seven observed lifecycle states have Edit, Start/Restart, Stop, and Delete decisions.
4. Search the design for accidental runtime-completion claims and retain
   `Design complete; implementation not started` status.
5. Run `git diff --check` and inspect the final diff for unrelated changes.

## Deferred Runtime Evidence

The design does not claim protobuf generation, Java/Go validation parity, connector option schemas,
OIDC/RBAC enforcement, CSRF handling, idempotency persistence, transactional mutation audit,
secret-reference migration, write routes, browser workflows, convergence polling, race tests,
deployment rollout, or production operation. Those are mandatory implementation gates before Slice
19 can be marked Complete.
