# ADR-019: Cooperative Cancellation Boundary

## Status

Accepted

## Date

2026-08-03

## Context

The Phase 0 runtime is synchronous and owns connector resources on one thread. It currently has
no way for an embedding caller to stop a long-running job between bounded pulls. Interrupting a
thread or closing a connector from another thread would race with JDBC transactions and violate
the explicit lifecycle ownership established by ADR-011 and ADR-014.

## Decision

The Engine exposes a small `CancellationToken` functional interface and adds it to
`SingleNodeSyncJob.Builder`; the default token never cancels, preserving existing callers. The
runtime checks the token at these cooperative boundaries:

1. before Source open;
2. after Source open and before Sink open;
3. before each Source pull;
4. after a batch is transformed and before Sink write.

If cancellation is observed, the runtime raises `SyncJobException` with stage `CANCELLED` and a
partial `SyncResult`, then closes every resource whose `open` completed in the existing reverse
order. A cancellation request cannot interrupt a connector call already in progress; connector
timeouts and database-specific interruption remain connector options. No background thread,
forced interrupt, or asynchronous close is introduced.

The token is embedding code and may itself fail. A `RuntimeException` raised while querying it is
not reported as a successful cancellation: the runtime wraps it in `SyncJobException` with stage
`CANCELLATION_CHECK`, the current partial `SyncResult`, and the original exception as cause. Opened
resources still close in reverse order and close failures remain suppressed on that structured
failure. The CLI categorizes this stage as `runtime`, not `cancelled`.

`LocalJobRunner` accepts an overload with a token while its existing `run(JobSpec)` method uses
the never-cancelled token. The CLI maps `CANCELLED` to exit code 5. Phase 0 does not install an OS
signal handler; an embedding application may connect its signal/shutdown mechanism to the token.

## Consequences

### Positive

- Cancellation has deterministic stage, partial counters, and resource-close evidence.
- A broken embedding callback cannot bypass the Engine failure and cleanup model.
- Existing synchronous backpressure and connector ownership remain unchanged.
- Embedders can implement timeouts or UI cancellation without a new executor abstraction.

### Negative

- A blocked Source or Sink is not forcibly interrupted and may delay observation.
- A cancellation observed after a batch was committed cannot undo that batch; the actual delivery
  behavior remains at-most-once with partial output risk.
- CLI process termination still depends on the host process and does not guarantee graceful
  cancellation.

## Alternatives Considered

### Interrupt the Runtime Thread

Rejected because JDBC and file drivers do not provide a portable interruption contract and an
interrupt can skip connector cleanup or leave transactions ambiguous.

### Close Connectors from Another Thread

Rejected because it breaks the single owner lifecycle and can race with `writeBatch` or commit.

### Add a Full Async Job Controller

Deferred to the distributed/runtime phases; Phase 0 needs only a boundary-level token.

## Related Decisions

- ADR-011: Bounded Pull-based Single-node Runtime
- ADR-014: Local Runner and CLI Boundary
- ADR-017: JDBC Transaction Boundaries
