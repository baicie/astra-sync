# Phase 6 Slice 19 Workflow Matrix

## Status

Design complete; runtime behavior awaits implementation.

## JobService Coverage

| RPC | Console entry | Permission | Version / idempotency | Success boundary |
|---|---|---|---|---|
| `CreateJob` | New Job editor | `jobs.create` | Idempotency key; no prior version | Version 1, `CREATED`, desired `STOPPED` committed |
| `GetJob` | Detail and editor load | `jobs.read` | None | Authorized redacted Job snapshot returned |
| `ListJobs` | Job table | `jobs.read` | None | One bounded tenant page returned |
| `UpdateJob` | Inactive Job editor | `jobs.update` | Positive expected version and idempotency key | Validated replacement spec and new version committed |
| `DeleteJob` | Detail destructive action | `jobs.delete` | Positive expected version and idempotency key | Job removed with retry tombstone committed |
| `StartJob` | Start or Restart action | `jobs.start` | Positive expected version and idempotency key | Desired `RUNNING` accepted; convergence is asynchronous |
| `StopJob` | Stop action | `jobs.stop` | Positive expected version and idempotency key | Desired `STOPPED` accepted; convergence is asynchronous |
| `GetJobStatus` | Detail refresh and convergence polling | `jobs.read` | None | Current authorized status snapshot returned |

All requests are scoped to the server-validated selected tenant. Read responses expose only the
public, secret-redacted Job projection. Authorization precedes Job lookup, so callers cannot use any
workflow to probe another tenant.

Because `GetJobStatus` does not contain the enclosing Job version, the Console refreshes `GetJob`
before an action confirmation and after every observed transition. It does not enable a later write
using the version cached before polling.

## Observed-state Action Matrix

The table describes normal Console controls. The API still handles exact retries idempotently even
when the corresponding control is disabled.

| Observed state | Edit | Start / Restart | Stop | Delete | Expected next visible state |
|---|---|---|---|---|---|
| `CREATED` | Enabled | Start | Disabled; already stopped | Enabled | Start -> `INITIALIZING` |
| `INITIALIZING` | Disabled | Disabled; starting | Enabled | Disabled | Stop -> `CANCELING` |
| `RUNNING` | Disabled | Disabled; running | Enabled | Disabled | Stop -> `CANCELING` |
| `CANCELING` | Disabled | Disabled | Disabled; stopping | Disabled | Controller -> `CANCELED` or `FAILED` |
| `CANCELED` | Enabled | Restart | Disabled; already stopped | Enabled | Restart -> `INITIALIZING` |
| `FINISHED` | Enabled | Restart | Disabled; already stopped | Enabled | Restart -> `INITIALIZING` |
| `FAILED` | Enabled | Restart | Disabled; already stopped | Enabled | Restart -> `INITIALIZING` |

Each cell is additionally gated by its permission. A stale Console can render an action that has
become invalid; the API lifecycle check wins and the Console refreshes state.

## Direct Command Semantics

| Command and current condition | API result | Version | Epoch | Audit outcome |
|---|---|---|---|---|
| Start from `CREATED`, `CANCELED`, `FINISHED`, or `FAILED` | Accept `INITIALIZING` / desired `RUNNING` | Increment | Increment | `CHANGED` |
| Start while already desired `RUNNING` in `INITIALIZING` or `RUNNING` | Return current Job | Unchanged | Unchanged | `NO_CHANGE` for a new request key |
| Start from `CANCELING` | `FailedPrecondition` | Unchanged | Unchanged | Rejected attempt policy |
| Stop from `INITIALIZING` or `RUNNING` | Accept `CANCELING` / desired `STOPPED` | Increment | Unchanged | `CHANGED` |
| Stop when desired state is already `STOPPED`, including `CANCELING` | Return current Job | Unchanged | Unchanged | `NO_CHANGE` for a new request key |
| Update or Delete in an active state | `FailedPrecondition` | Unchanged | Unchanged | Rejected attempt policy |
| Exact replay of a completed mutation key and digest | Return stored operation result | Unchanged | Unchanged | Reuse original mutation event |
| Reuse of a key with another digest | `AlreadyExists` / `IDEMPOTENCY_KEY_REUSED` | Unchanged | Unchanged | Security-relevant rejected attempt |

Desired-state no-op detection happens before treating the previous version carried by an exact
retry as stale. A new state-changing request still requires the current positive expected version.

## Workflow Preconditions

| Operation | Canonical validation | Confirmation | Additional precondition |
|---|---|---|---|
| Create | Complete submitted JobSpec | Submit from reviewed editor | Name is unused in tenant |
| Update | Complete replacement JobSpec | Save from reviewed editor | Inactive state and matching version |
| Start / Restart | Stored JobSpec under current compiler revision | Execution-impact summary | Startable state and matching version |
| Stop | Not required | Cooperative-cancellation impact | Stoppable state or idempotent stopped result |
| Delete | Not required | Exact typed Job name | Inactive state, matching version, delete permission |

Client-side field checks never satisfy the canonical-validation column. A successful earlier
preflight is advisory at mutation time because connector inventory and Job version can change.

## Client Operation States

| Client state | Primary presentation | Allowed operator response |
|---|---|---|
| `editing` | Dirty or clean draft | Change fields, validate, cancel |
| `validating` | Stable editor dimensions with pending indicator | Cancel request; keep draft |
| `invalid` | Field and summary issues with stable codes | Correct draft and revalidate |
| `submitting` | One disabled primary action | Wait; do not mint a new key |
| `accepted` | Returned version, request ID, and accepted desired state | Continue to detail |
| `reconciling` | Current observed state and elapsed time | Refresh or navigate away; writes stay disabled |
| `terminal` | `CANCELED`, `FINISHED`, or `FAILED` details | Edit, Restart, or Delete if authorized |
| `timed_out` | Accepted command; convergence unknown | Manual refresh, not blind resubmit |
| `conflict` | Base and latest versions, preserved draft | Reload, discard, or explicitly reapply |
| `unknown_outcome` | Request ID and retry-safe guidance | Retry with same key or read current Job |

Dynamic labels, validation text, or status refreshes must not resize the action bar or obscure Job
state. Only one mutation for a selected Job is submitted by one browser session at a time; the API
still handles concurrent clients independently.

## Failure and Recovery Matrix

| Failure point | Known fact | Recovery |
|---|---|---|
| Local validation fails | Nothing sent | Preserve draft and focus first issue |
| Canonical preflight rejects | No mutation committed | Preserve draft and render redacted typed issues |
| Authorization rejects | Mutation did not run | Refresh session/permissions; do not reveal resource existence |
| Version conflict | This payload did not commit | Fetch latest, preserve draft, require explicit reapply |
| Lifecycle precondition rejects | This payload did not commit | Refresh Job state and offer only newly valid actions |
| HTTP/gRPC timeout before definitive response | Outcome unknown | Retry same key; then read Job/status |
| Idempotency replay returns accepted result | Original transaction committed | Refresh Job/status before further action |
| Start/Stop polling fails | Desired command may remain committed | Keep accepted marker and allow manual refresh |
| Status changes but full Job refresh fails | New expected version is unknown | Keep write controls disabled and retry full refresh |
| Start converges to `FAILED` | New epoch reached terminal failure | Show bounded failure details and retain Restart action |
| Stop remains `CANCELING` after timeout | Cancellation is still requested | Show timeout without offering force kill |
| Delete response is lost | Tombstone may exist | Retry same key and accept stored success |
| Validator unavailable | No new mutation may commit | Fail closed with bounded retry guidance |

## Race Ordering

1. Authorization and typed tenant resolution occur before repository lookup.
2. Basic shape checks run, then the idempotency key is claimed or replayed before expensive work.
3. Expected version and lifecycle checks use the current row.
4. Create, Update, and Start canonical validation uses the exact spec that would execute.
5. The Job change or deletion, idempotency completion, and audit event commit atomically.
6. The response is derived from the committed result; Start/Stop observation happens through later
   reads and service-actor reconciliation events.

Implementations may validate before opening the final database transaction, but must recheck the
version, lifecycle, idempotency claim, and validation digest inside the bounded commit path. No
network connector I/O is allowed while holding the transaction.
