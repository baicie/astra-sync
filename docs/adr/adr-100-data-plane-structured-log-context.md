# ADR-100: Data-Plane Structured Log Context

## Status

Accepted — request identity carrier added by ADR-102.

## Context

The observability handbook defines stable log fields for `tenant_id`,
`job_id`, `epoch`, `stage`, and `outcome`, but Java data-plane call sites did
not attach those values to MDC. Coordinator and Worker JSON logs therefore
contained the `component` field while task identity remained absent.

Copying identity fields into every log message would duplicate data and make
it easy to omit a field. The context must instead be scoped to an execution
boundary and cleaned up even when execution fails.

## Decision

Add `DataPlaneLogContext` as the shared Java MDC scope helper:

1. Open a task or execution context with trusted `tenant_id`, `job_id`, and an
   optional positive `epoch`.
2. Support nested stage and outcome scopes.
3. Snapshot previous MDC values and restore them on close.
4. Ignore blank values and non-positive epochs.
5. Log Coordinator execution start and completion with tenant/job/epoch.
6. Log Worker task start and completion/failure with tenant/job identity.
7. Set `stage=read` for the source thread, `stage=write` for the sink thread,
   and `stage=checkpoint` for checkpoint execution.
8. Set `request_id` only when a trusted, non-unknown Coordinator execution or
   Worker request value is available. ADR-102 adds the Worker protocol carrier.

The context contains identity and lifecycle metadata only. It must not contain
credentials, connector options, SQL text, or free-form user input.

## Consequences

- Coordinator and Worker JSON log records can be joined on the same bounded
  tenant/job fields used by metrics.
- Nested read/write/checkpoint stages restore the outer context when complete.
- Worker executor threads receive stage-specific context independently.
- Coordinator-side remote task dispatch uses the same identity context as the
  Worker task it invokes.
- ADR-101 adds `worker_id` to the same nested context for multi-Worker
  correlation.
- ADR-102 adds `request_id` to the same context and carries it through normal
  and checkpoint Worker requests.
- Failure paths emit a structured warning before the original exception
  propagates.
- No metric, protocol, deployment, or dependency change is required.
- OIDC request correlation remains separate. Java data-plane `request_id`
  exemplar emission is added by ADR-103.

## Rollback

Remove `DataPlaneLogContext` usage from Coordinator and Worker execution
boundaries. Existing error logging and all protocol and metric behavior remain
otherwise unchanged.
