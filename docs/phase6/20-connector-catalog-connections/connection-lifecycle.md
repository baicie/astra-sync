# Phase 6 Slice 20 Connection Lifecycle

## Status

Implemented lifecycle and API contract. Production mutation admission remains disabled by default.

## Resource Identity and Projection

A Connection belongs to exactly one tenant and one canonical connector. Its identity fields are:

| Field | Semantics |
|---|---|
| `tenant_id` | Server-resolved immutable tenant UUID |
| `name` | Immutable DNS-label name unique in the tenant |
| `uid` | Random immutable UUID used by internal foreign keys |
| `connector` | Immutable canonical connector name |
| `version` | Positive optimistic compare-and-swap version |
| `generation` | Positive immutable connector-effective configuration generation |
| `state` | `DISABLED` or `ACTIVE` administrative state |
| `compatibility` | Computed `COMPATIBLE`, `REVALIDATION_REQUIRED`, or `CONNECTOR_UNAVAILABLE` |

The authorized public projection also contains display name, description, descriptor revision,
timestamps, non-secret values allowed by descriptor sensitivity, provider kind,
`secret_configured`, last bounded test summary, and reference counts appropriate to the caller. It
never contains an external Secret name, namespace, provider path, provider token, key mapping,
credential value, or historical locator.

`version` protects API mutations. `generation` snapshots connector-effective settings. Updating
display text changes only version; changing non-secret endpoint settings, descriptor contract, or
Secret locator appends a generation and changes both.

## State Model

```text
Create
  |
  v
DISABLED -- Enable --> ACTIVE
    ^                    |
    +------ Disable -----+

DISABLED or ACTIVE -- Rotate --> same state, generation + 1
DISABLED -- Update effective settings --> DISABLED, generation + 1
DISABLED -- Delete (only when unreferenced) --> tombstone
```

Compatibility is orthogonal to administrative state. `ACTIVE + COMPATIBLE` is the only effective
state admitted for a new Job binding or Start. Catalog rollout can make an active Connection
incompatible without silently changing its state. Running epochs remain bound to their accepted
generation and artifact.

Create defaults to `DISABLED`. Enable proves only current metadata completeness and compatibility;
it does not perform a network test. A test is optional evidence tied to one generation and never
automatically enables, disables, rotates, or deletes a Connection.

## API Semantics

### Create

`CreateConnection` requires tenant, immutable name and connector, complete Connection-owned
non-secret values, a write-only external Secret binding when required, idempotency key, and no
caller-selected UID/state/version/generation. Admission:

1. authorizes `connections.create` in the authenticated tenant;
2. loads the current descriptor and validates the Connection schema without provider I/O;
3. rejects raw secret fields and unknown/Job-owned options;
4. validates provider and locator shape against server-owned tenant policy;
5. atomically creates version 1, generation 1, state `DISABLED`, idempotency result, and audit; and
6. returns a redacted projection.

An exact retry returns the original UID. A name collision and an idempotency digest mismatch are
distinct stable errors. Create never provisions or mutates an external Secret.

### Read and List

`GetConnection` and `ListConnections` require `connections.read`, apply tenant scope before lookup,
and return redacted projections. List uses stable `(name, uid)` ordering, bounded page size, and a
token bound to tenant, filters, policy revision, and list snapshot. A denied caller cannot
distinguish missing from cross-tenant identity.

Connection reads do not imply `connections.use`. A user may inspect safe metadata but still be
unable to bind the Connection to a Job.

### Update

`UpdateConnection` requires `connections.update`, positive expected version, idempotency key, and a
complete replacement of mutable fields. Identity, connector, Secret binding, state, UID, and
tenant are not updateable through this method.

Display metadata can be replaced in either administrative state. Changing any connector-effective
non-secret value requires `DISABLED` and a complete replacement that is compatible with the current
descriptor; it appends a generation. Partial merge-patch and masked-value round trips are not used.
Version conflict preserves the caller draft and exposes only the authorized current version.

### Rotate

`RotateConnection` requires `connections.rotate`, expected version, idempotency key, and a complete
new write-only external Secret binding. It does not accept secret bytes. The target must identify a
new immutable or provider-versioned Secret object; in-place mutable aliases are rejected for the
initial Kubernetes provider.

Rotation appends a generation containing the current non-secret settings and new restricted
locator, then advances current generation atomically with audit. Administrative state is
unchanged. A rotation committed after a Start cannot change that epoch; the next changed Start
captures the new generation. Old locators remain only while a live/retained epoch may need them.

### Enable and Disable

`EnableConnection` requires the Connection availability permission, expected version, and an
idempotency key. It checks the current descriptor, schema compatibility, required Secret-binding
presence, and tenant provider policy without reading secret bytes. It commits `ACTIVE` and audit.
A connectivity test is not required and an old successful test is not treated as proof of current
reachability.

`DisableConnection` uses the same CAS and idempotency rules and commits `DISABLED`. It prevents new
Job Create/Update bindings and new changed Start transactions. It does not revoke an already
accepted epoch, mutate its binding, delete its materialized Secret, or issue Stop. A caller that
must end running work uses the separately authorized Job lifecycle.

Repeated Enable/Disable at the requested state is an authorized `NO_CHANGE` with no version bump
and an audit outcome correlated to the idempotency result.

### Delete

`DeleteConnection` requires `DISABLED`, `connections.delete`, expected version, idempotency key,
and all of these proofs in the deletion transaction:

- no source or sink `job_connection_binding` references the UID;
- no nonterminal or retention-protected `execution_connection_binding` references a generation;
- no queued or running Connection test references a generation; and
- no materialization cleanup obligation remains.

Failure returns bounded reference counts and stable reason, never inaccessible Job names. Success
deletes current metadata and eligible generations, writes an idempotency tombstone and audit event,
and preserves only retention-approved identity evidence. It never deletes the provider Secret.
Reusing the same human name later creates a new UID and cannot capture an old binding.

## Job Binding

`ConnectorConfig.connection_ref` remains the user-facing tenant-local Connection name. Job Create
and Update resolve each non-empty source/sink reference under the same transaction and store:

```text
job UID + endpoint role -> connection UID + connector name
```

Admission requires `connections.use`, exact tenant, exact connector name, requested role support,
`ACTIVE`, and current schema compatibility. Unknown, cross-tenant, unauthorized, disabled, and
missing references produce the same external `CONNECTION_REF_UNAVAILABLE` issue after normal
authorization. Internally, metrics may distinguish bounded categories without including names.

A Job read can retain the opaque `connection_ref` with `jobs.read`; it does not expose the
Connection projection. Renaming Connections is disallowed so the stored display reference and UID
binding cannot drift. Job Update atomically replaces endpoint bindings. Job Delete removes them in
the Job transaction.

## Epoch Snapshot

A changed `StartJob` transaction revalidates the stored bindings and writes:

```text
job UID + epoch + role
  -> connection UID + generation + descriptor revision + compiler revision
```

This is the boundary for rotation and disable races. The transaction locks or compare-and-swaps
the relevant Connection rows in stable UID order so both source and sink are captured from one
consistent admission decision. Rotation or disable waits, then applies to later epochs. A Start
desired-state no-op neither allocates an epoch nor captures a new generation.

Scheduler, materializer, and runtime consume only the epoch binding. They cannot look up `latest`
or resolve by Connection name. A missing retained generation is an integrity failure, not a reason
to substitute current credentials.

## Connection Test Operation

`TestConnection` requires `connections.test`, expected Connection version, and an idempotency key.
It captures UID, generation, connector/descriptor revision, actor, tenant egress policy, deadline,
and random operation ID. It returns quickly with `QUEUED`; `GetConnectionTest` reads one of:

```text
QUEUED -> RUNNING -> SUCCEEDED
                  -> FAILED
                  -> TIMED_OUT
                  -> CANCELED
any terminal result -> EXPIRED after retention
```

Tests are limited per tenant, actor, Connection, and destination policy. One probe has a hard
deadline, bounded DNS answers, connection attempts, redirects, bytes, and response text. It may
perform transport/TLS/authentication/read-only handshake checks defined by the exact connector
artifact. It cannot enumerate catalogs, list tables, sample records, execute caller SQL, mutate a
destination, create replication slots/publications, or become a general network proxy.

The result contains operation ID, captured generation, timestamps, stable phase/code, success
boolean, and safe remediation key. Vendor text, endpoint addresses, resolved IPs, credentials, and
stack traces are excluded. Completion writes a service-actor audit event. Rotation while a test
runs leaves the result attached to the old generation and marks it non-current in presentation.

## Concurrency and Idempotency

Connection writes reuse the Slice 19 idempotency model: scope by tenant, actor, full method, and
random key; bind to deterministic request digest; commit successful result and required audit in
the same transaction; reject key reuse with another digest; and retain Delete tombstones for at
least the retry window.

Expected version is checked before every changed mutation. Exact same-state requests may return
`NO_CHANGE` after confirming authorization and request identity. Database locks are acquired in
tenant/Connection UID order, and Job Start captures source then sink by sorted Connection UID to
avoid deadlocks. Provider calls and network tests never occur while a PostgreSQL mutation
transaction is open.

## Stable Failures

| Reason | Typical code | Meaning |
|---|---|---|
| `CONNECTOR_UNAVAILABLE` | `FailedPrecondition` | Current deployment does not expose the connector |
| `CONNECTION_SCHEMA_INCOMPATIBLE` | `FailedPrecondition` | Stored generation is not accepted by current descriptor |
| `CONNECTION_REF_UNAVAILABLE` | `NotFound` or validation issue | Missing, inaccessible, disabled, or wrong-tenant reference |
| `CONNECTION_VERSION_CONFLICT` | `Aborted` | Expected version is stale |
| `CONNECTION_IN_USE` | `FailedPrecondition` | Job, epoch, test, or cleanup reference blocks deletion |
| `SECRET_BINDING_INVALID` | `InvalidArgument` | Provider locator shape/policy is invalid; value is not echoed |
| `SECRET_MATERIAL_UNAVAILABLE` | `FailedPrecondition` | Captured provider object cannot be resolved at test/dispatch |
| `TEST_LIMIT_EXCEEDED` | `ResourceExhausted` | Tenant/actor/destination test budget is exhausted |
| `IDEMPOTENCY_KEY_REUSED` | `AlreadyExists` | Same key has another request digest |

Public messages never echo Connection values, refs, locators, endpoints, Job names from another
scope, or vendor errors. Clients branch on gRPC code and stable reason only.

## Retention

Connection test details expire after a bounded period, initially 24 hours; audit evidence follows
the tenant audit policy. Superseded generations remain while referenced by nonterminal executions
and for a bounded post-terminal debugging/rollback window. A background reconciler deletes only
unreferenced restricted locator records after the configured retention period and records counts,
not values.

Provider Secrets are governed by their owning platform process. AstraSync surfaces when a locator
is no longer materializable but does not extend, rotate, or delete provider retention.
