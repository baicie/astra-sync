# Phase 6 Slice 20 Authorization and Audit Matrix

## Status

Implementation-ready extension to the Slice 18 policy vocabulary. Runtime enforcement is not
implemented.

## Permission Vocabulary

| Permission | Scope | Meaning |
|---|---|---|
| `connectors.read` | Tenant | Read the deployment connector catalog visible to one tenant |
| `connections.read` | Tenant | Read redacted Connection summaries/details and safe test summaries |
| `connections.create` | Tenant | Create a disabled Connection that references an external Secret |
| `connections.update` | Tenant | Replace display or disabled non-secret Connection settings |
| `connections.use` | Tenant | Bind a compatible active Connection to a Job and capture it at Start |
| `connections.test` | Tenant | Start and inspect an isolated test of one Connection generation |
| `connections.rotate` | Tenant | Point a Connection at a new immutable external Secret generation |
| `connections.disable` | Tenant | Change administrative availability with Enable or Disable |
| `connections.delete` | Tenant | Delete an eligible disabled, unreferenced Connection |

`connections.disable` names the availability-management boundary and covers both Enable and
Disable so one actor cannot undo another availability decision through a weaker update permission.
It does not grant `jobs.stop` or provider-side credential revocation.

No permission reveals Secret locators or bytes. Provider provisioning remains an external platform
permission and is not implied by any AstraSync tenant role.

## Built-in Role Additions

| Role | Slice 20 additions |
|---|---|
| `tenant_viewer` | `connectors.read` |
| `tenant_operator` | `connectors.read`, `connections.read`, `connections.use`, `connections.test` |
| `tenant_auditor` | `connectors.read` |
| `tenant_admin` | All tenant-scoped connector and Connection permissions |
| `platform_admin` | All tenant permissions, still evaluated against an explicit active tenant |

Existing Job, member, audit, and platform permissions remain unchanged. `tenant_operator` can use
and test pre-provisioned Connections for normal Job work but cannot change shared credentials or
availability. Connection administration is initially limited to `tenant_admin`; a future custom
role system can separate it without weakening method-level policy.

`tenant_auditor` reads Connection-related audit events through `audit.read`, not Connection detail.
`tenant_viewer` can see catalog fields needed to understand a Job connector but cannot enumerate
Connection endpoint metadata.

## Connector Catalog Service Mapping

| Full gRPC method | Permission | Scope source | Audit policy |
|---|---|---|---|
| `ConnectorCatalogService/ListConnectorDescriptors` | `connectors.read` | Request tenant | Policy-controlled successful read; always denied event |
| `ConnectorCatalogService/GetConnectorDescriptor` | `connectors.read` | Request tenant | Policy-controlled successful read; always denied event |

The Catalog API has no public mutation method. Deployment inventory publication accepts only the
catalog reconciler service identity through a separate internal method registry.

## Connection Service Mapping

| Full gRPC method | Permission | Scope source | Audit policy |
|---|---|---|---|
| `ConnectionService/CreateConnection` | `connections.create` | Request tenant | Required transactional mutation event |
| `ConnectionService/GetConnection` | `connections.read` | Request tenant | Policy-controlled successful read; always denied event |
| `ConnectionService/ListConnections` | `connections.read` | Request tenant | Policy-controlled successful read; always denied event |
| `ConnectionService/UpdateConnection` | `connections.update` | Request tenant | Required transactional mutation event |
| `ConnectionService/RotateConnection` | `connections.rotate` | Request tenant | Required transactional mutation event |
| `ConnectionService/EnableConnection` | `connections.disable` | Request tenant | Required transactional mutation event |
| `ConnectionService/DisableConnection` | `connections.disable` | Request tenant | Required transactional mutation event |
| `ConnectionService/DeleteConnection` | `connections.delete` | Request tenant | Required transactional mutation event |
| `ConnectionService/TestConnection` | `connections.test` | Request tenant | Required request event plus terminal service event |
| `ConnectionService/GetConnectionTest` | `connections.test` | Request tenant | Policy-controlled successful read; always denied event |

Generated fully qualified method constants are the policy-registry keys. A completeness test fails
for any generated public or internal RPC without exactly one scope and policy entry.

## Additional JobService Checks

| Job operation | Existing permission | Conditional Connection check |
|---|---|---|
| `CreateJob` | `jobs.create` | `connections.use` for every non-empty reference |
| `UpdateJob` | `jobs.update` | `connections.use` for every replacement reference |
| `StartJob` | `jobs.start` | `connections.use` for every durable binding before epoch capture |
| `GetJob`, `ListJobs`, `GetJobStatus` | `jobs.read` | No Connection detail permission; reference stays opaque |
| `StopJob`, `DeleteJob` | Existing Job permission | No new use check; deletion removes bindings transactionally |

Both permissions must be present; neither implies the other. A Job mutation cannot use platform
service identity to bypass the initiating actor's `connections.use`. Authorization revision is
part of validation/idempotency cache identity.

## Internal Service Identities

| Identity | Allowed behavior | Explicitly denied |
|---|---|---|
| Catalog publisher | Publish exact deployment inventory to internal reconciler | Tenant APIs, Connection data, Secret access |
| Catalog reconciler | Validate/activate descriptor snapshots | Connector upload, Secret access, Job mutation |
| Canonical validator | Read descriptor and redacted resolved metadata | Secret locator/bytes, provider/network I/O |
| Scheduler | Read dispatch and epoch bindings for leased work | End-user Connection CRUD, arbitrary generation/latest lookup |
| Materializer | Resolve exact restricted locator and create epoch Secret | List all Secrets, mutate provider Secret, public API |
| Test executor | Resolve captured generation and perform bounded probe | Job lifecycle mutation, unrestricted egress, write probes |
| Runtime pod | Read only mounted epoch envelope | PostgreSQL Connection rows, provider API, other epochs |
| Cleanup reconciler | Delete validated stale execution resources | External provider Secret deletion |

Internal identities use authenticated workload identity and explicit method allowlists. A service
actor always carries tenant, Job/test operation, and causating request IDs into audit; being internal
does not permit cross-tenant substitution.

## Audit Events

| Event | Actor | Transaction / correlation | Allowlisted fields |
|---|---|---|---|
| `connection.create` | User | Connection/idempotency transaction | UID, connector, version/generation, descriptor revision, outcome |
| `connection.update` | User | Connection/idempotency transaction | UID, before/after version/generation, changed field keys, outcome |
| `connection.rotate` | User | Connection/idempotency transaction | UID, before/after generation, provider kind, outcome |
| `connection.enable` | User | Connection/idempotency transaction | UID, before/after state/version, compatibility, outcome |
| `connection.disable` | User | Connection/idempotency transaction | UID, before/after state/version, affected binding counts, outcome |
| `connection.delete` | User | Delete tombstone transaction | UID, final version/generation, reference counts, outcome |
| `connection.use` | User | Same transaction as Job Create/Update/Start | UID, role, Job UID, epoch when Start, generation, outcome |
| `connection.test.request` | User | Test admission/idempotency transaction | UID, operation ID, generation, policy revision, outcome |
| `connection.test.complete` | Service | Test terminal transaction | UID, operation ID, generation, stable phase/code, duration |
| `connection.materialize` | Service | Receipt/dispatch correlation | UID, Job UID, epoch, role, generation, provider kind, stable outcome |
| `connector.inventory.activate` | Service | Inventory activation | Old/new inventory revision, descriptor counts, outcome |

Raw Connection names are included only when audit policy requires a human resource label; stable
UID is preferred. Events never contain non-secret setting values, endpoints, JobSpec, Connection
reference strings, Secret locator/name/UID/key mapping, provider version details beyond an opaque
receipt token, vendor errors, credential bytes, or request/response bodies.

Authorized no-ops produce `NO_CHANGE`. Denied attempts follow ADR-037 synchronous security audit
with redacted target fingerprints. A failed authorization cannot reveal whether a Connection,
tenant, Secret, or test exists.

## Decision Rules

1. Authenticate and resolve tenant before resource lookup or page-token decoding.
2. Deny inactive principal, membership, tenant, or stale authorization revision.
3. Require explicit tenant membership even for a platform administrator performing tenant work.
4. Resolve `connection_ref` only after Job permission and `connections.use` both pass.
5. Compare immutable tenant UUIDs, never namespace/name similarity.
6. Enforce Connection state, compatibility, expected version, Job version, and epoch independently
   of authorization success.
7. Do not weaken denial because the same principal can read a Job that contains an opaque reference.
8. Recheck authorization on idempotent replay; a cached successful result cannot bypass revoked
   permission.
9. Audit failure aborts public mutations. Service-side materialization/test audit failure blocks
   launch or completion publication and triggers cleanup/retry.
