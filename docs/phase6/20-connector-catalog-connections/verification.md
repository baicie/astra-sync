# Phase 6 Slice 20 Design Verification

## Status

Design complete; runtime verification awaits implementation.

## Design Checks

| Check | Result |
|---|---|
| Current Java descriptor/registry and deployment artifact authority inventoried | PASS |
| Existing protobuf/Go/CRD `connection_ref` fields and Scheduler rejection inventoried | PASS |
| Legacy YAML-only Connection CRD explicitly excluded as an implementation baseline | PASS |
| Catalog list/get ownership, projection, pagination, and tenant scope specified | PASS |
| Descriptor, inventory, Connection schema, and compiler revisions distinguished | PASS |
| Option ownership, sensitivity, extension-prefix, and compatibility rules specified | PASS |
| Connection identity, state, CAS version, immutable generation, and deletion rules specified | PASS |
| Job name reference to stable UID and Start epoch-generation snapshot specified | PASS |
| Rotation, disable, Start, retry, and descriptor rollout races specified | PASS |
| External Secret provider stores no bytes in JobSpec/PostgreSQL/public surfaces | PASS |
| Kubernetes provider immutability, tenant mapping, materialization, and cleanup specified | PASS |
| Permissions, built-in roles, method coverage, service identities, and audit specified | PASS |
| Connection tests separated from canonical validation and isolated against SSRF/writes | PASS |
| Migration, feature gates, rollout, rollback, and completion evidence specified | PASS |
| Runtime capability claims excluded from Design Complete status | PASS |

## Public RPC Traceability

| RPC | Lifecycle evidence | Authorization/audit evidence | Safety/concurrency evidence |
|---|---|---|---|
| `ListConnectorDescriptors` | Descriptor Public Catalog RPCs | Catalog mapping | Revision-bound tenant pagination |
| `GetConnectorDescriptor` | Descriptor Public Catalog RPCs | Catalog mapping | Current/retained distinction and redaction |
| `CreateConnection` | Lifecycle Create | Connection mapping and `connection.create` | Disabled default, schema check, idempotency |
| `GetConnection` | Lifecycle Read and List | Connection mapping | Redacted same-tenant projection |
| `ListConnections` | Lifecycle Read and List | Connection mapping | Bounded stable tenant page |
| `UpdateConnection` | Lifecycle Update | Connection mapping and `connection.update` | CAS and disabled effective update |
| `RotateConnection` | Lifecycle Rotate | Connection mapping and `connection.rotate` | Immutable generation and Start race |
| `EnableConnection` | Lifecycle Enable and Disable | Availability permission and audit | Metadata-only compatibility and CAS |
| `DisableConnection` | Lifecycle Enable and Disable | Availability permission and audit | Future-epoch boundary and CAS |
| `DeleteConnection` | Lifecycle Delete | Connection mapping and tombstone audit | Binding/test/cleanup reference proof |
| `TestConnection` | Lifecycle Test Operation | Test permission and request audit | Captured generation, idempotency, budgets |
| `GetConnectionTest` | Lifecycle Test Operation | Test permission/read audit | Redacted terminal/expiry state |

## Lifecycle Matrix

| Current condition | Update effective settings | Rotate | Enable | Disable | Bind/Start | Delete |
|---|---|---|---|---|---|---|
| `DISABLED + COMPATIBLE` | Allowed, new generation | Allowed, new generation | Allowed | No change | Denied | Only if unreferenced |
| `ACTIVE + COMPATIBLE` | Denied until disabled | Allowed, new generation | No change | Allowed | Allowed | Denied |
| `DISABLED + REVALIDATION_REQUIRED` | Allowed only as explicit compatible replacement | Allowed but still incompatible unless schema fixed | Denied | No change | Denied | Only if unreferenced |
| `ACTIVE + REVALIDATION_REQUIRED` | Denied until disabled | Allowed but availability remains incompatible | Denied | Allowed | Denied | Denied |
| Connector unavailable | Display-only update | Locator rotation allowed but not useful | Denied | Allowed | Denied | Only disabled and unreferenced |
| Referenced by any Job | State rules apply | State rules apply | State rules apply | Allowed | State/compatibility rules apply | Denied |
| Referenced by accepted/running epoch | State rules apply | New generation does not alter epoch | State rules apply | Does not stop epoch | Existing epoch fixed | Denied until retention permits |

## Race Traceability

| Race | Winning boundary | Required evidence |
|---|---|---|
| Job Update vs Connection Delete | Stable binding foreign key + transaction | Delete cannot leave a dangling UID |
| Start vs Rotate | Connection row lock/CAS and epoch binding transaction | Epoch uses exactly one committed generation |
| Start vs Disable | Transaction order | Start accepted first proceeds; Disable first blocks Start |
| Catalog rollout vs Start | Active compiler/inventory revision | Start captures one compatible revision or fails |
| Scheduler retry vs rotation | Epoch binding, never current lookup | Retry uses captured generation |
| Secret delete/recreate vs materialize | Pinned provider UID and immutable requirement | New object is rejected |
| Lease loss vs Secret creation | Lease-bound cancellation + reconciliation | No Coordinator launch by stale owner |
| Delete vs test/materialization cleanup | Reference/obligation rows | Delete waits or fails with bounded reason |

## Requirement Traceability

| Requirement | Design evidence |
|---|---|
| Catalog reflects executable deployment | Design ownership and descriptor authority/publication |
| Tenants cannot upload code | Descriptor authority and no public mutation RPC |
| Stable compatibility | Descriptor revisions and compatibility matrix |
| Same-tenant stable reference | Lifecycle Job Binding and authorization decision rules |
| No secrets in control-plane records | Security trust boundary and redaction table |
| Deterministic rotation/retry | Lifecycle Epoch Snapshot and security rotation semantics |
| Least-privilege materialization | Provider contract, Kubernetes provider, service identities |
| Canonical validation remains side-effect free | Design Job Validation integration and isolated test boundary |
| Auditable administration/use | Authorization audit event matrix |
| Safe deletion and cleanup | Lifecycle Delete, deterministic resources, failure handling |
| No premature runtime support | README and implementation feature gate |

## Static Verification Procedure

Before merging this design change:

1. Resolve every relative Markdown link in the Slice 20 directory, Phase 6 index, ADR index, and
   architecture baseline.
2. Confirm the public RPC traceability table names both Catalog methods and all ten Connection
   methods exactly once.
3. Confirm permission mappings cover every public method and conditional Job use check.
4. Confirm lifecycle rows cover both administrative states, all compatibility outcomes,
   references, and accepted epochs.
5. Search for conflicting claims that PostgreSQL stores secret bytes, canonical validation performs
   I/O, rotation changes a running epoch, or the legacy CRD is implemented.
6. Retain exact `Design complete; implementation not started` status in the Slice README.
7. Run `git diff --check`, Markdown link validation, formatting checks, and inspect the final diff
   for unrelated changes.

## Deferred Runtime Evidence

The design does not claim descriptor protobuf generation, connector option metadata, catalog
publication, PostgreSQL schema, Connection APIs, RBAC enforcement, transactional audit, Job UID
bindings, epoch generation capture, Secret provider isolation, Kubernetes materialization, runtime
configuration merge, connectivity tests, Console workflows, legacy migration, race tests, cleanup,
or production rollout. Each is a mandatory implementation gate before Slice 20 can be marked
Complete or Scheduler `connectionRef` rejection can be removed.
