# ADR-038: Desired-state Job Mutation Workflows

## Status

Accepted

## Context

ADR-029 established a durable Job record, optimistic versions, desired-state Start/Stop commands,
and epoch-fenced observed transitions. Slice 17 exposed only reads, and Slice 18 designed the tenant
permissions and transactional audit boundary required for writes. Exposing raw mutation buttons
without a shared workflow contract would still leave operators vulnerable to silent lost updates,
ambiguous network timeouts, duplicate destructive requests, and the false impression that an
accepted Start or Stop had already converged.

Restart and force-operation APIs could also create a second lifecycle vocabulary that diverges from
the Controller and Scheduler state machine.

## Decision

Use the existing `JobService` and ADR-029 lifecycle as the only Job mutation authority. Create
persists a stopped Job and never starts it implicitly. Update replaces the complete spec only while
the Job is inactive. Delete is allowed only while inactive and requires destructive confirmation.
Start and Stop remain desired-state commands; the Console displays their committed response as
accepted and observes later convergence with `GetJobStatus`.

Present `StartJob` as Restart when the current observed state is `CANCELED`, `FINISHED`, or `FAILED`.
A changed Start increments epoch through the existing domain transition. Do not add a separate
restart state or RPC. Do not add force stop or force delete in this slice.

Require Console Update, Start, Stop, and Delete requests to carry the current positive Job version.
On conflict, retain any edit draft, fetch the latest authorized Job, and require explicit reload or
reapply; never silently overwrite. Desired-state commands preserve their existing no-op semantics
for exact or independently repeated matching requests.

Require a random idempotency key on every externally retried mutation. Scope it by tenant, actor,
and method, bind it to a request digest, and atomically commit its bounded result with the Job change
and required ADR-037 audit event. Exact replay returns the original result. Reuse with another
digest fails. Delete retains a bounded tombstone so a lost success response can be replayed without
turning into ambiguous `NotFound`.

The Console uses one presentation state machine for editing, validation, submission, acceptance,
reconciliation, terminal observation, timeout, conflict, and unknown outcome. This state is not
persisted as Job lifecycle state. API authorization and lifecycle checks remain authoritative even
when the UI hides or disables a control.

## Consequences

- Console and direct API workflows retain the same durable state, version, and epoch semantics.
- Operators can distinguish a committed desired-state command from runtime convergence.
- Concurrent edits become explicit recovery decisions rather than last-write-wins updates.
- Mutation retries need PostgreSQL idempotency storage, retention, concurrency control, and bounded
  response/tombstone representations.
- Job mutation repositories must join Job, idempotency, and audit writes in one unit of work.
- Start/Stop polling adds read traffic and requires bounded backoff and timeout behavior.
- Force termination, bulk operations, rollback, and savepoint workflows remain separate decisions
  with their own consistency and authorization requirements.
