# Phase 6 Slice 19 Design: Job Mutation Workflows and Operational Actions

## Status

Authoritative design implemented by the Phase 6 platform delivery. Production mutation exposure
remains subject to identity, TLS, audit, and Connection rollout gates.

## Goals

1. Let an authorized operator create, validate, edit, start, stop, restart, and delete one
   tenant-scoped Job through the Console.
2. Reuse the existing protobuf `JobService`, durable Job record, desired-state lifecycle, optimistic
   version, and execution epoch.
3. Make every accepted command, asynchronous transition, conflict, timeout, and retry visible
   without claiming that command acceptance means execution convergence.
4. Validate JobSpecs against the same connector inventory and capability rules used to compile an
   execution plan.
5. Require explicit permissions, CSRF protection, idempotency, transactional audit, and secret-safe
   payload handling on every mutation path.
6. Define implementation, rollout, rollback, and verification gates before writes are enabled.

## Non-goals

- Batch Job mutations, bulk selection, templates, import/export, or scheduled actions.
- Log or metric streaming, worker topology, lineage, alerting, or audit-event presentation.
- Savepoints, pause/resume, force kill, rollback to an older JobSpec, or manual epoch selection.
- Creating, editing, rotating, or revealing connection credentials.
- Connectivity tests that open a source, sink, file, socket, or database transaction.
- A second lifecycle state machine, a Console-owned Job store, or client-authoritative validation.
- Completing the connector registry product surface; this slice defines only the descriptor fields
  needed to construct and validate one JobSpec.

## Dependencies and Preserved Invariants

Slice 19 depends on the Slice 18 authentication, tenant RBAC, Console BFF session, same-origin CSRF,
and transactional audit design. Mutation routes must not be enabled in production until those
controls are implemented and verified.

The following existing behavior remains authoritative:

- PostgreSQL stores the durable Job and compare-and-swap version.
- `UpdateJob` and `DeleteJob` operate only on a caller-supplied expected version.
- `StartJob` and `StopJob` are idempotent desired-state commands.
- A changed Start increments the epoch; a repeated matching Start does not.
- Specification update and deletion are rejected in `INITIALIZING`, `RUNNING`, and `CANCELING`.
- Controller, Scheduler, and Coordinator transitions remain epoch fenced.
- Namespace is the tenant slug and is resolved from the authenticated session plus server-validated
  selection, never from an untrusted browser override.

## Topology

```text
browser
  | secure session + CSRF + same-origin request
  v
Console BFF
  | OIDC access token, tenant, request ID, idempotency key, expected version
  +------------------------------+
  v                              v
JobService admission        JobValidationService
  | authz + lifecycle            | structural validation
  | canonical validation         | Java JobCompiler + descriptors
  | transaction                  | no connector I/O
  v                              v
Job repository + idempotency + audit     validation result
  |
  v
Controller / Scheduler reconciliation -> GetJobStatus polling
```

The API Server remains the final authorization and mutation authority. Console visibility and
disabled-button rules are usability controls only. Canonical validation is a shared backend
capability invoked by both explicit preflight requests and mutation admission; the browser never
decides whether a JobSpec is executable.

## Console Information Architecture

The existing list and detail views remain the entry point. Slice 19 adds:

- `/jobs/new`: one create editor for the selected tenant;
- `/jobs/{name}/edit`: replacement editor seeded from an inactive Job version;
- a detail action bar with state- and permission-aware Start/Restart, Stop, Edit, and Delete
  controls;
- a validation summary anchored to fields and a compact operation-status region for accepted,
  reconciling, terminal, timed-out, and failed states.

The editor groups source, sink, delivery, transforms, and runtime settings. Connector fields are
generated from server descriptors. Job name and tenant become immutable after creation. Leaving an
editor with a changed draft requires a normal unsaved-change guard; server data is never silently
written by auto-save.

Effective permissions returned through the authenticated Console session determine which controls
are rendered. Current Job state determines whether a rendered control is enabled. The API repeats
both checks for every request.

## Console BFF Contract

The BFF exposes same-origin JSON adapters and supplies the authenticated tenant to protobuf calls:

| Method | Path | Upstream operation |
|---|---|---|
| `POST` | `/api/job-validations` | `JobValidationService.ValidateJobSpec` |
| `GET` | `/api/connectors` | `JobValidationService.ListConnectorDescriptors` |
| `POST` | `/api/jobs` | `JobService.CreateJob` |
| `PUT` | `/api/jobs/{name}` | `JobService.UpdateJob` |
| `POST` | `/api/jobs/{name}/start` | `JobService.StartJob` |
| `POST` | `/api/jobs/{name}/stop` | `JobService.StopJob` |
| `DELETE` | `/api/jobs/{name}` | `JobService.DeleteJob` |

Existing list, detail, and status routes remain unchanged. Every write requires a valid session,
same-origin request, session-bound CSRF token, bounded JSON body, `application/json`, request ID,
and `Idempotency-Key`. Update, Start, Stop, and Delete also require the currently displayed positive
version, mapped to protobuf `expected_version`. The BFF ignores a browser-supplied namespace and
adds the server-validated selected tenant.

Protobuf contracts gain an additive `idempotency_key` field on each mutation request and the new
validation service. The BFF maps `Idempotency-Key` into that field; direct gRPC or generated-gateway
clients set the field explicitly. Request correlation remains transport metadata generated or
validated at the trusted ingress. Generated service descriptors stay the policy-registry source of
truth so every new RPC receives a typed tenant scope and permission.

## Common Mutation State Machine

The client operation state is presentation state, not a durable Job lifecycle:

```text
editing -> validating -> submitting -> accepted -> reconciling -> observed/terminal
   ^           |             |            |             |
   |           +-- invalid   +-- rejected +-- timeout   +-- refresh error
   +---------------- conflict/reload/reapply ----------------+
```

`accepted` means the API transaction committed. For Create, Update, and Delete, that transaction is
the operation boundary. For Start and Stop, it means only that desired state was durably accepted;
the Controller and Scheduler still need to converge observed state.

The browser disables duplicate submission while a request is in flight but retains the same
idempotency key until the operation obtains a definitive response or the user changes the request.
A transport timeout is shown as an unknown outcome. The BFF retries with the same key or reads the
Job; it never sends a newly keyed mutation merely because a response was lost.

`GetJobStatus` does not currently carry the enclosing Job version. Status polling therefore cannot
provide the expected version for a later mutation. Before opening any action confirmation, and
after any observed lifecycle transition, the Console refreshes the full `GetJob` snapshot. Action
controls remain disabled until that refresh supplies the current version; compare-and-swap still
protects the race after the refresh.

## Create Workflow

1. Require `jobs.create`, resolve the selected tenant server-side, and load connector descriptors.
2. Generate one draft ID and idempotency key. Collect the immutable Job name and descriptor-backed
   JobSpec without accepting raw credential fields.
3. Run local shape checks for immediate field feedback, then call canonical preflight.
4. On explicit submit, the API reruns or safely reuses canonical validation bound to the exact
   canonical spec digest and current compiler revision. A browser result alone is never trusted.
5. Atomically create version 1 in `CREATED` / desired `STOPPED`, store the idempotency result, and
   append the `job.create` audit event.
6. Navigate to detail using the returned Job. `AlreadyExists` keeps the draft and offers navigation
   to the existing Job only after an authorized read confirms it.

Create does not automatically Start. Separating persistence from execution lets an operator review
the stored, redacted representation and produces distinct create and start audit evidence.

## Edit Workflow

1. Render Edit only with `jobs.update` and an inactive observed state. Load a fresh Job before the
   editor opens and retain its version as the draft base.
2. Keep name, tenant, UID, status, epoch, and timestamps read-only. Replace only `JobSpec`.
3. Validate the complete replacement spec; partial patch semantics are not introduced.
4. Submit `expected_version`, idempotency key, and complete spec. The API validates before one
   transaction updates the Job, records the idempotency result, and writes `job.update` audit.
5. If the version is stale, preserve the local draft, fetch the latest authorized Job, and show the
   base version and latest version. Offer Reload latest, Discard draft, or Reapply draft to latest.

Reapply never submits automatically. It seeds a new draft on the latest version, reruns validation,
and requires a new explicit submission. There is no force overwrite and no silent last-write-wins
path. A state change to an active state is a lifecycle precondition failure even when the version
still matches.

## Start and Restart Workflow

The detail action is labeled Start for `CREATED` and Restart for `CANCELED`, `FINISHED`, or `FAILED`.
Both call `StartJob`; there is no separate restart API.

Before submission, the UI shows source, sink, delivery guarantee, current state, current epoch, and
the fact that a successful changed command creates a new epoch. The API requires `jobs.start`, a
positive expected version, canonical validation against the current deployment compiler profile,
and an idempotency key.

A changed command commits desired `RUNNING`, observed `INITIALIZING`, incremented epoch, version,
idempotency result, and `job.start` audit atomically. A matching command already satisfied by
`INITIALIZING` or `RUNNING` is a successful no-op and does not increment version or epoch. A restart
clears the previous terminal failure from current status while retained audit and execution history
remain the historical record.

After acceptance, the Console polls authorized status at one second initially, backs off to at most
five seconds, and stops automatic polling after two minutes or navigation away. `RUNNING` is the
successful convergence signal; `FAILED` is terminal failure. On an observed transition, it reloads
the full Job before enabling Stop, Edit, Restart, or Delete so the next command has a current
version. Poll timeout says that the command was accepted but convergence is not yet known and leaves
manual Refresh available.

## Stop Workflow

Stop requires `jobs.stop`. The confirmation names the Job and explains that AstraSync will request
cooperative cancellation; it does not promise immediate process termination or rollback of writes
already committed by the execution.

For `INITIALIZING` or `RUNNING`, an accepted changed command stores desired `STOPPED`, observed
`CANCELING`, a new Job version, idempotency result, and `job.stop` audit atomically. The execution
epoch does not change. Repeated Stop while desired state is already stopped is a successful no-op.

The Console polls until `CANCELED` or `FAILED`, then reloads the full Job before enabling another
action. `CANCELING` remains visible as an in-progress state. Timeout and lost-response recovery
follow Start semantics. Force termination is intentionally not offered; adding it requires a
separate fencing and data-consistency decision.

## Delete Workflow

Delete requires `jobs.delete`, which the built-in `tenant_operator` role does not have. It is
available only for inactive Jobs. The destructive confirmation displays tenant and name, requires
the operator to type the exact Job name, and states that execution artifacts, checkpoints, or
external destination data are not implicitly removed unless a later retention contract says so.

The request carries the latest expected version and idempotency key. The Job row deletion,
idempotency tombstone, and `job.delete` audit event commit in one transaction. The tombstone contains
stable Job identity, before version, outcome, and a bounded response, never the JobSpec. An exact
retry therefore returns the original success instead of ambiguous `NotFound`.

An active-state failure offers Stop, not force delete. A version conflict closes the confirmation,
preserves no destructive intent, reloads detail, and requires the operator to confirm again.

## Optimistic Concurrency and Idempotency

Every Console mutation uses the version currently displayed. For changed Start and Stop commands,
the API checks that version before mutation. Existing desired-state idempotency remains: a matching
command can return the current state even when a retry carries the preceding version.

Idempotency records are scoped by tenant, actor, full method, and random key. They contain a request
digest, state (`IN_PROGRESS` or `COMPLETED`), operation result reference, audit event ID, and bounded
expiry of at least 24 hours. The record participates in the same database transaction as a domain
mutation. Reuse with a different request digest fails with stable reason
`IDEMPOTENCY_KEY_REUSED`; it never executes the second payload. Raw keys are fingerprinted before
audit or logs.

The API claims `IN_PROGRESS` with a short owner lease after authentication and basic request-shape
checks. A successful change completes the record atomically with domain and audit state. A
deterministic validation, conflict, or lifecycle rejection may complete a bounded error result so
an exact retry is stable; it has no successful mutation event. `Unavailable`, deadline, and
pre-commit internal failures release the claim or let its lease expire so the same key can retry.
Takeover first proves that no completed transaction exists.

Create replay returns the originally created Job identity. Update, Start, and Stop replay returns
the original accepted response and the Console immediately refreshes current Job state. Delete
replay returns the original success from its tombstone. A matching desired-state command with a new
key is a separately authorized no-op and is audited with before and after versions equal.

## Errors and Recovery

The API uses standard gRPC codes plus typed details with stable reason, field path, current version,
current state, request ID, and retry guidance where disclosure is authorized. The Console maps them
as follows:

| Condition | gRPC / HTTP | Console behavior |
|---|---|---|
| Invalid structure or canonical issue | `InvalidArgument` / 400 | Anchor issues to fields and preserve draft |
| Missing login | `Unauthenticated` / 401 | End local session and begin login flow |
| Missing permission or tenant | `PermissionDenied` / 403 | Remove stale controls and refresh identity |
| Missing Job | `NotFound` / 404 | Return to list; preserve create/edit draft when useful |
| Duplicate name or reused key | `AlreadyExists` / 409 | Show stable reason; never guess which request won |
| Stale version | `Aborted` / 409 | Load latest and offer explicit reload/reapply choices |
| Lifecycle or validation precondition | `FailedPrecondition` / 412 | Refresh state/descriptors and explain blocked action |
| Admission limit | `ResourceExhausted` / 429 | Honor bounded retry guidance with same key |
| Dependency or deadline failure | `Unavailable` / 503 | Mark outcome unknown and retry/read with same key |

Error details and UI messages never echo raw option values, credentials, tokens, request bodies, or
full JobSpecs. A status polling failure does not relabel an already accepted command as rejected.

## Authorization and Audit

Permissions remain those defined by Slice 18: `jobs.create`, `jobs.update`, `jobs.start`,
`jobs.stop`, and `jobs.delete`. Validation uses the permission matching its declared purpose;
descriptor listing requires an authenticated tenant member with `jobs.read`. The API rejects an
unknown validation purpose and never lets a caller use a read permission to test a mutation-only
spec or inaccessible connection reference.

Successful changes write `job.create`, `job.update`, `job.start`, `job.stop`, or `job.delete` in the
same transaction as Job and idempotency state. Authorized no-ops record outcome `NO_CHANGE`.
Allowlisted metadata includes request ID, idempotency-key fingerprint, validation ID and codes,
before/after version, before/after desired and observed state, and epoch. It excludes raw JobSpecs,
connector options, connection values, error stack traces, and response bodies.

Denied authorization attempts and validation failures follow Slice 18 synchronous audit policy but
cannot create a Job mutation event. Later Controller and Scheduler transitions continue to use
service actors and reference the causating desired-state version and request ID when available.

## Feature Gate and Rollout

`CONSOLE_JOB_MUTATIONS_ENABLED` defaults to false. Production startup refuses to enable it unless
OIDC/RBAC enforcement, TLS, server-side tenant selection, CSRF validation, canonical validation,
idempotency persistence, and required transactional audit are all enabled. The flag controls route
registration as well as UI rendering.

Rollout proceeds through validation-only shadow observation, authenticated staging writes, lifecycle
and audit reconciliation, then one-tenant production enablement before broader rollout. Rollback
disables new Console writes and leaves accepted desired state untouched; operators still use the
authenticated API for explicit recovery. Rollback never switches production to anonymous access or
silently reverses a Start, Stop, Update, or Delete.

## Acceptance Criteria

- Every one of the eight current `JobService` RPCs has a documented Console/API workflow,
  permission, tenant scope, lifecycle rule, and error behavior.
- Create persists a validated stopped Job and never starts it implicitly.
- Edit and Delete reject active Jobs and stale versions without losing or silently overwriting an
  edit draft.
- Start and Restart use `StartJob`, increment epoch only on a changed command, and distinguish API
  acceptance from observed convergence.
- Stop remains cooperative, idempotent, and visible through `CANCELING` to a terminal state.
- Lost responses are recoverable with one idempotency key; key reuse with another payload is denied.
- Browser checks and descriptor forms cannot bypass canonical server-side compilation validation.
- Raw secrets do not enter JobSpecs, browser responses, validation issues, audit, idempotency rows,
  or logs; pre-provisioned references are tenant authorized.
- Console controls and API authorization agree, while direct API tampering is independently denied.
- Production cannot register write routes before authentication, CSRF, validation, idempotency,
  transactional audit, and TLS gates pass.
