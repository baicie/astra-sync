# Phase 6 Slice 20 Design: Connector Catalog and Tenant Connection References

## Status

Authoritative design implemented by the Slice 20 delivery. Production rollout remains explicitly
gated and is not implied by this document or the repository merge.

## Goals

1. Publish the connector inventory actually available to canonical validation and execution.
2. Give authenticated tenants a stable, redacted, versioned Connection resource for connector
   settings that should not be copied into every Job.
3. Keep secret bytes outside PostgreSQL, JobSpec, protobuf responses, browser state, logs, audit,
   traces, and idempotency records.
4. Resolve a Job's human-readable `connection_ref` to a stable same-tenant Connection UID and
   capture an immutable Connection generation for every accepted execution epoch.
5. Make descriptor rollout, credential rotation, disablement, deletion, and dispatch races have
   explicit compatibility and fencing semantics.
6. Define least-privilege permissions, transactional audit, isolated connection testing, staged
   rollout, rollback, and runtime completion evidence.

## Non-goals

- Tenant-uploaded connector JARs, arbitrary binaries, a connector marketplace, or hot classloading.
- Storing, returning, editing, generating, or recovering password, token, key, or certificate
  bytes through an AstraSync public API.
- Automatic credential rotation or ownership of an external Secret's lifecycle.
- Cross-tenant or platform-wide shared Connections.
- Schema discovery, table browsing, data preview, query execution, or write testing.
- Making network access part of canonical JobSpec validation.
- Replacing the Java `ConnectorRegistry`, `JobCompiler`, Job lifecycle, Scheduler lease, or epoch
  fence with a Console-owned model.
- Treating the existing YAML-only Connection CRD as an implemented or authoritative contract.

## Dependencies and Preserved Invariants

Slice 20 builds on ADR-013 descriptor-first planning, ADR-029 durable Jobs, ADR-030 lease-fenced
dispatch, ADR-036 tenant identity, ADR-037 transactional audit, and ADR-039 side-effect-free
canonical validation. Runtime implementation depends on the Slice 18 and Slice 19 security and
mutation gates even if additive catalog contracts are implemented earlier.

The following invariants remain authoritative:

- PostgreSQL owns tenant, Job, Connection, version, binding, and audit metadata.
- Deployed Java artifacts own executable connector behavior and descriptor content.
- `connection_ref` is resolved only in the authenticated Job tenant; browser namespace input is
  ignored.
- A Job mutation never stores secret material and a catalog read never resolves a Secret.
- Canonical validation performs metadata-only checks and no connector or network I/O.
- One accepted Start allocates one epoch. That epoch uses immutable Connection generations even if
  a Connection is later rotated or disabled.
- Coordinator and Worker processes still verify connector artifact versions before opening a
  connector.
- Current Scheduler rejection remains until the entire materialization path is enabled.

## Target Topology

```text
deployed connector artifacts
        |
        v
Java descriptor publisher / canonical validator
        | authenticated internal inventory
        v
API Server catalog reconciler -> immutable descriptor snapshots -> Catalog API
        |                                      |
        |                                      v
        |                               Console forms
        v
Connection API -> PostgreSQL metadata + external Secret locator
        |                         |
        |                         +----> Kubernetes Secret (bytes stay external)
        v
Job mutation -> stable Job/Connection binding
        |
        v
Start transaction -> epoch/Connection-generation binding
        |
        v
Scheduler lease -> isolated materializer -> immutable epoch Secret -> data-plane pods
```

The Java inventory is the content authority. PostgreSQL stores immutable snapshots so reads,
pagination, audit correlation, and compatibility decisions have stable evidence; it does not let
an API user invent or modify a descriptor.

## Ownership Boundaries

| Concern | Authority |
|---|---|
| Connector implementation, roles, capabilities, option schema | Deployed Java artifact |
| Active deployment inventory and execution profile | Platform deployment configuration |
| Public catalog projection and tenant visibility | API Server |
| Connection identity, state, version, generation metadata | PostgreSQL Connection repository |
| Secret bytes and provider-side versions | External Secret provider |
| Job-to-Connection stable binding | Job mutation transaction in PostgreSQL |
| Epoch-to-generation snapshot | Start transaction in PostgreSQL |
| Credential retrieval and Kubernetes materialization | Scheduler service identity and provider adapter |
| Connector creation and final artifact fence | Coordinator/Worker runtime |
| End-user identity, permission, and audit | Slice 18 authorization and audit boundary |

## Public Service Surface

Slice 20 introduces dedicated protobuf-first services. The Slice 19 Console route
`GET /api/connectors` becomes an adapter over `ConnectorCatalogService`; the earlier provisional
ownership under `JobValidationService` does not need to be implemented.

| Service | RPC | Purpose |
|---|---|---|
| `ConnectorCatalogService` | `ListConnectorDescriptors` | List a revision-consistent, bounded tenant-visible inventory |
| `ConnectorCatalogService` | `GetConnectorDescriptor` | Read one current or retained descriptor revision |
| `ConnectionService` | `CreateConnection` | Create a disabled Connection and first immutable generation |
| `ConnectionService` | `GetConnection` | Read one redacted Connection projection |
| `ConnectionService` | `ListConnections` | List bounded redacted summaries in one tenant |
| `ConnectionService` | `UpdateConnection` | Replace mutable metadata or non-secret settings with CAS |
| `ConnectionService` | `RotateConnection` | Bind a new external Secret and create a generation |
| `ConnectionService` | `EnableConnection` | Admit future Job bindings and execution epochs |
| `ConnectionService` | `DisableConnection` | Block future bindings and epochs without stopping current work |
| `ConnectionService` | `DeleteConnection` | Delete only when no Job or live operation references it |
| `ConnectionService` | `TestConnection` | Start an isolated, bounded test of one captured generation |
| `ConnectionService` | `GetConnectionTest` | Poll a redacted, expiring test result |

All request namespaces are resolved from authenticated tenant context at the Console BFF and
rechecked by the API. Direct gRPC requests carry an explicit tenant field that is policy scoped.
Every write uses a positive expected version where a resource already exists and a random
idempotency key. Pagination tokens bind tenant, filters, policy revision, and inventory or list
revision and are opaque to clients.

## Persistent Model

The initial PostgreSQL model has six core logical records:

| Record | Durable purpose |
|---|---|
| `connector_descriptor_snapshot` | Immutable canonical public descriptor keyed by descriptor revision |
| `connector_inventory` | One ordered set of descriptor revisions for a deployment/profile revision |
| `connection` | Tenant identity, stable UID/name, state, optimistic version, and current generation |
| `connection_generation` | Immutable connector revision, non-secret settings, restricted provider locator, and generation number |
| `job_connection_binding` | Job UID plus source/sink role to stable Connection UID |
| `execution_connection_binding` | Job UID/epoch/role to immutable Connection generation and descriptor revision |

Connection names are DNS-label compatible and unique per tenant. UIDs are random and never reused.
The JobSpec retains its user-facing `connection_ref`, while the binding table prevents a later
same-name resource from silently capturing an old Job. A Job Create or Update transaction writes
the Job and both role bindings atomically after authorization and compatibility checks.

`connection.version` is the compare-and-swap version for API mutations. `generation` changes only
when connector-effective non-secret settings, connector contract revision, or Secret locator
changes. Display metadata can increment version without creating a generation. Generations are
append-only while referenced by a live or retained execution.

Secret locators are operational metadata, not secret bytes. They are stored in a restricted
column/record, excluded from generic serializers and public projections, and retained only as long
as their generation can be materialized or is required by retention policy.

## Descriptor and Connection Compatibility

Every public descriptor has a canonical `descriptor_revision`; every inventory has an
`inventory_revision`. A Connection generation records its connector name and
`connection_schema_revision`. The current descriptor explicitly lists accepted Connection schema
revisions. Compatibility never relies only on a semantic-version comparison or a matching name.

An effective Connection is usable only when all are true:

1. its administrative state is `ACTIVE`;
2. its connector remains in the selected deployment inventory;
3. the descriptor supports the requested source or sink role;
4. the connector name exactly matches the Job endpoint connector;
5. the descriptor accepts the generation's Connection schema revision;
6. the caller has `connections.use`; and
7. the Job and Connection tenant IDs are equal.

A descriptor-only label or documentation change can publish a new descriptor revision while
retaining the same accepted Connection schema. A breaking schema change leaves old generations
visible but unavailable for new bindings or Starts until an explicit update/migration creates a
compatible generation. Running epochs continue against their captured artifact and generation.

## Job Validation and Mutation Integration

Descriptor schema assigns every connector option to exactly one owner: `JOB` or `CONNECTION`.
Sensitive options must be `CONNECTION` owned. The public Job API rejects a sensitive or
Connection-owned key in `JobSpec.options`, even when its value is blank or masked.

For Create and Update, the Go admission layer resolves each non-empty `connection_ref` to a
same-tenant UID, verifies `connections.use`, state, connector, role, and schema compatibility, and
constructs redacted resolved metadata for canonical validation. Java validation receives
non-secret effective values and secret-key presence, never secret bytes or provider locators. It
validates the same descriptor and compiler rules used by runtime planning without opening a
connector.

For Start, the API loads the stored Job and stable bindings, repeats current compatibility and
authorization checks, runs canonical validation against the active inventory, allocates the epoch,
and writes `execution_connection_binding` rows in the same transaction. Rotation after that commit
does not change the accepted epoch. A desired-state no-op reuses the existing epoch bindings.

The deterministic effective runtime configuration is assembled only after materialization:

1. descriptor defaults;
2. captured Connection generation non-secret values;
3. captured generation secret values; and
4. Job-owned options.

The descriptor makes these key sets disjoint. Duplicate ownership, unknown keys, and attempts to
override a Connection or secret key fail before connector creation.

## Runtime Dispatch Integration

The Scheduler reads execution bindings under its existing lease and epoch fence. A provider
adapter resolves each immutable locator through service identity, then creates an immutable,
execution-scoped Kubernetes Secret with deterministic UID/epoch identity. Secret values are mounted
as read-only files into only the data-plane pods for that epoch; they are not placed in JobSpec,
command-line arguments, environment variables, Kubernetes labels, events, or pod annotations.

Dispatch stores a materialization receipt containing tenant, Job UID, epoch, role, Connection UID,
generation, descriptor revision, provider kind/version token, and generated Kubernetes Secret
UID/resource version. It does not store secret bytes or a reusable value digest. Scheduler retry
for the same epoch verifies and reuses the same immutable materialization. A different generation
cannot overwrite it.

Provider resolution failure is a fenced dispatch failure with a stable redacted reason. It never
falls back to raw Job options, a latest Connection generation, another tenant, or a prior epoch.
Cleanup removes execution-scoped Secrets with the other deterministic epoch resources. External
Secrets are not deleted by AstraSync.

## Administrative Lifecycle

Create produces `DISABLED`, generation 1. This lets an administrator review and optionally test a
Connection before use. Enable performs metadata-only compatibility checks and changes the state to
`ACTIVE`; it does not claim network reachability. Update of connector-effective settings requires
`DISABLED` and creates a generation. Rotate may replace the external immutable Secret locator in
either state and creates a generation.

Disable blocks later Job bindings and Start transactions but does not mutate accepted epoch
bindings or stop running work. To stop an execution, an operator uses `StopJob`. Delete is rejected
while any Job binding, nonterminal epoch, pending test, or retained materialization obligation
exists. Deletion never deletes the provider Secret.

Connectivity tests are separate asynchronous operations. The API captures one generation, applies
tenant rate and concurrency limits, and delegates to an isolated data-plane probe with a strict
deadline and egress policy. The probe may perform only connector-defined DNS, transport, TLS,
authentication, and read-only handshake steps. Results use stable sanitized codes and expire; they
do not change Connection state automatically.

## Console Boundary

The Console gains a connector catalog view and tenant Connection list/detail/create/edit/rotate/
enable/disable/test/delete workflows only after Slice 18 is implemented. Descriptor-driven forms
render Job-owned and Connection-owned fields separately. Secret inputs accept an external Secret
reference, never secret bytes; existing locators render only as configured/not configured.

Controls are permission and state aware, but API enforcement is independent. Rotate, disable, and
delete use explicit confirmations. Tests show queued/running/succeeded/failed/expired status and
never display vendor exception text. Job editors list only active, role-compatible Connections,
while a stored unavailable reference remains visible as a redacted blocking issue.

## Failure and Availability Rules

| Condition | Required behavior |
|---|---|
| Catalog publisher unavailable | Serve last verified catalog reads; fail canonical writes that require the validator |
| Inventory changes during pagination | Reject continuation with `CATALOG_REVISION_CHANGED` |
| Connector removed or incompatible | Block new binding/Start; keep running epochs and retained evidence |
| Connection disabled after Start commit | Accepted epoch proceeds; later Starts fail until enabled |
| Rotation races with Start | Start uses the generation committed in its transaction |
| Secret missing or provider denied | Fail materialization with redacted reason; do not try another generation |
| Scheduler retries same epoch | Reuse/verify deterministic immutable materialization |
| Materializer partially creates resources | Reconciliation deletes or completes only same UID/epoch resources |
| Test executor unavailable | Test fails or expires; Connection and Job state remain unchanged |
| Audit transaction fails | Public Connection mutation commits nothing |

Already running data-plane work does not depend on Catalog API or Connection API availability.
Creating, updating, or starting a Job fails closed if current metadata compatibility cannot be
established. A control-plane outage never causes secret bytes to be copied into a fallback payload.

## Existing Connection CRD

`deployment/operator/config/crd/bases/sync.astrasync.io_connections.yaml` has no Go type,
repository, controller, tenant authorization, version contract, or Secret materialization path. Its
free-form host/username/properties shape also conflicts with descriptor-owned schemas. It is an
unimplemented historical artifact, not a migration source or public promise.

Implementation first establishes the protobuf/PostgreSQL contract in this design. The legacy CRD
remains unused and must not be installed as evidence of support. A later Kubernetes-facing adapter
may generate a new CRD from the same domain contract or remove the old manifest in a separately
reviewed compatibility change.

## Feature Gate and Completion Claim

Catalog reads, Connection administration, testing, and runtime materialization use separate feature
gates. Runtime `connection_ref` support remains off until the implementation plan's end-to-end
gates pass. Rollback disables new Connection writes and new Starts that require references while
allowing already materialized epochs to finish or be stopped.

Slice 20 may be marked Complete only when the public contracts, RBAC, audit, storage, migration,
provider isolation, epoch snapshot, Scheduler reconciliation, runtime merge, redaction sentinels,
cross-tenant tests, rotation races, cleanup, and staged production evidence all exist.
