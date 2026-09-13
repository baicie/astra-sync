# ADR-100: Data-Plane Structured Log Context

## Status

Accepted

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
8. Keep `request_id` propagation separate; no request identifier exists in the
   Worker task contract.

The context contains identity and lifecycle metadata only. It must not contain
credentials, connector options, SQL text, or free-form user input.

## Consequences

- Coordinator and Worker JSON log records can be joined on the same bounded
  tenant/job fields used by metrics.
- Nested read/write/checkpoint stages restore the outer context when complete.
- Worker executor threads receive stage-specific context independently.
- Failure paths emit a structured warning before the original exception
  propagates.
- No metric, protocol, deployment, or dependency change is required.
- OIDC request correlation and `request_id` exemplar emission remain separate
  follow-up work.

## Rollback

Remove `DataPlaneLogContext` usage from Coordinator and Worker execution
boundaries. Existing error logging and all protocol and metric behavior remain
otherwise unchanged.
