# Phase 6 Slice 20 Connector Descriptor Contract

## Status

Implementation-ready contract. The current Java `ConnectorDescriptor` exposes only name, artifact
version, roles, and capabilities; the fields below are additive design requirements.

## Authority and Publication

`ConnectorRegistry.discover()` continues to load deployment-bundled `ConnectorFactory` providers
with `ServiceLoader` and reject duplicate canonical names. Each factory returns an immutable,
side-effect-free descriptor. A Java internal publisher serializes the validated inventory in a
deterministic protobuf form used by the canonical validator, API catalog reconciler, Coordinator,
and contract tests.

The API accepts inventory only from an authenticated deployment service identity and only for a
configured execution profile. There is no public Create, Update, or Delete descriptor RPC. A
tenant, Console, database row, Kubernetes custom resource, or remote URL cannot add executable
code or override descriptor content.

The catalog reconciler validates the complete inventory before activation:

- unique canonical names and valid artifact versions;
- deterministic ordering and serialization;
- supported descriptor schema version;
- role/capability invariants already enforced by `ConnectorDescriptor`;
- unique option keys and unambiguous option ownership;
- no secret default, public echo, or Job-owned sensitive field;
- valid connection requirements for each supported role;
- exact descriptor revision and inventory digest; and
- agreement with the deployment's compiler and execution profile revision.

An invalid inventory is never partially published. The last verified inventory remains readable,
but validation and mutation admission do not pretend that stale data is executable.

## Public Catalog RPCs

### `ListConnectorDescriptors`

The request contains tenant scope, optional role/capability filters, bounded page size, and opaque
page token. The response contains `inventory_revision`, `compiler_revision`, ordered descriptors,
and an optional next token. Results are sorted by canonical connector name and artifact version.

The first implementation exposes the same deployment inventory to every active tenant with
`connectors.read`. The response is still tenant scoped so future entitlements can filter it without
changing method shape. A token binds tenant ID, filters, policy revision, inventory revision,
cursor, and expiry. If the inventory changes, continuation fails with
`CATALOG_REVISION_CHANGED`; pages from two inventories are never mixed.

### `GetConnectorDescriptor`

The request identifies connector name and optionally an exact retained descriptor revision. An
empty revision means current. Exact historical reads exist only for authorized Job/Connection
diagnostics and return `NotFound` when retention has expired. They do not make an inactive
connector selectable or executable.

Unknown and tenant-hidden connectors are externally indistinguishable. Responses support normal
conditional caching with inventory/descriptor revision, but caches are private and tenant/policy
scoped.

## Canonical Descriptor Shape

| Field | Contract |
|---|---|
| `descriptor_schema_version` | Version of this public descriptor message, initially `1` |
| `descriptor_revision` | SHA-256 of deterministic canonical public descriptor bytes |
| `name` | Stable lowercase canonical connector identity, maximum 128 characters |
| `artifact_version` | Immutable release version of executable connector code |
| `display_name` | Safe human label, not an identity or lookup key |
| `description_key` | Documentation/localization key, never deployment internals |
| `roles` | Non-empty set of `SOURCE` and/or `SINK` |
| `capabilities` | Existing runtime capability enum values |
| `execution_modes` | Supported `BATCH` and/or `CDC` modes derived consistently from capabilities |
| `delivery_constraints` | Descriptor hints backed by canonical compiler enforcement |
| `options` | Ordered exact-key option definitions |
| `option_prefixes` | Explicit bounded extension namespaces; absent means unknown keys are rejected |
| `connection_requirements` | `NONE`, `OPTIONAL`, or `REQUIRED` per role |
| `connection_schema_revision` | Digest of connector-effective Connection field contract |
| `accepted_connection_schema_revisions` | Explicit revisions usable by the current artifact |

Public descriptors exclude Java class names, artifact paths, pod images, environment variables,
service addresses, tenant Secret locators, credentials, stack traces, and provider configuration.

`artifact_version` must not be reused with different executable behavior or descriptor bytes in one
release channel. The catalog rejects a name/version pair whose previously observed descriptor
revision changes. A new build publishes a new artifact version even for a metadata-only change.

## Option Definition

Each exact option definition contains:

| Field | Rules |
|---|---|
| `key` | Stable connector option key |
| `roles` | Roles for which the option applies |
| `owner` | Exactly one of `JOB` or `CONNECTION` |
| `value_type` | `STRING`, `INTEGER`, `BOOLEAN`, `DURATION`, or `ENUM` |
| `required` | Required for each listed role after defaults and Connection merge |
| `default_value` | Canonical non-secret default, omitted for sensitive fields |
| `enum_values` | Closed ordered values for `ENUM` |
| `minimum` / `maximum` | Numeric or duration bounds where applicable |
| `min_length` / `max_length` | String bounds where applicable |
| `pattern_key` | Stable server-owned validator identifier, not an arbitrary regex from a tenant |
| `sensitivity` | `PUBLIC`, `RESTRICTED`, or `SECRET` |
| `advanced` | Presentation hint only |
| `label_key` / `help_key` | Safe presentation keys, not trusted HTML |

`SECRET` requires `owner=CONNECTION`, no default, no enum values containing credentials, and no
public value projection. `RESTRICTED` covers operational metadata such as hosts or database names:
it may be stored as a Connection value but is omitted from ordinary logs and audit. `PUBLIC` means
safe for an authorized resource response, not safe for unauthenticated publication.

The schema may define typed `one_of`, `requires`, and `conflicts_with` relationships by option key.
General scripts, expressions, remote schemas, and arbitrary regular expressions are not accepted.
Semantic role, capability, delivery, and cross-field behavior remains enforced by the Java
validator and `JobCompiler`.

An `option_prefix` definition must bound key pattern, count, value size, owner, roles, and
sensitivity. It cannot overlap an exact key or another prefix. Control-plane persistence rejects
an undeclared advanced property even if a local connector currently accepts it. This is especially
important for vendor pass-through namespaces that may otherwise smuggle credentials.

## Connection Requirements

Connection schema is the role-specific subset of options with `owner=CONNECTION`, plus the Secret
provider requirements needed to satisfy `SECRET` fields. A connector can declare:

| Requirement | Job behavior |
|---|---|
| `NONE` | `connection_ref` must be empty for that role |
| `OPTIONAL` | Inline Job-owned options are valid; a compatible Connection may supply shared fields |
| `REQUIRED` | A same-tenant active compatible `connection_ref` is mandatory |

The same connector may differ by role. Every Connection binds exactly one connector name but may be
usable for any role whose current descriptor accepts its stored schema revision. The catalog never
assumes that two connector names can share credentials, even if both happen to use JDBC.

## Revisions

Three revisions serve different purposes:

| Revision | Inputs | Use |
|---|---|---|
| `descriptor_revision` | Canonical public descriptor | Conditional reads, diagnostics, immutable snapshots |
| `inventory_revision` | Sorted connector name, artifact version, and descriptor revision set | Stable listing and deployment inventory identity |
| `compiler_revision` | Inventory revision, JobSpec schema, Java compiler build, execution profile | Canonical validation cache and Start fencing |

`connection_schema_revision` is embedded in the descriptor and hashes only Connection-owned field
semantics. It can remain stable across a label, help text, Job-only option, or unrelated capability
change. The current descriptor lists every earlier revision it can consume without reinterpretation.

All hashes use deterministic protobuf serialization with documented normalization. Maps are sorted,
sets are emitted in enum order, absent and default fields are not conflated, and human locale does
not affect bytes. Clients treat revisions as opaque strings.

## Compatibility Decisions

| Change | Required result |
|---|---|
| Label/help text only | New descriptor/artifact revision; Connection schema may remain compatible |
| Add optional Job-owned option | Existing Connections remain compatible |
| Add optional Connection-owned field with safe default | New schema may explicitly accept old revision |
| Add required Connection or Secret field | Old revision is not accepted until explicit migration |
| Rename, remove, reinterpret, or change sensitivity of a Connection field | Breaking schema revision |
| Remove role/capability | New Starts requiring it fail; running epochs continue |
| Remove connector from inventory | No new binding or Start; retained descriptors support diagnostics only |
| Change artifact under same name/version | Deployment/catalog activation fails |

Compatibility is explicit and one-way: a new artifact declares which old Connection revisions it
can consume. The platform never guesses compatibility from semantic version, JSON shape, or a
successful historical test. An incompatible Connection reports `REVALIDATION_REQUIRED`; an absent
connector reports `CONNECTOR_UNAVAILABLE`. Neither condition mutates the administrative state or
stops an accepted epoch.

## Deployment Skew

The active inventory represents only artifacts available to every Scheduler-selected runtime pool
for its execution profile. During rolling deployment, a descriptor is not activated until the
validator and eligible Coordinator/Worker images report compatible inventory. Start captures the
compiler and descriptor revisions; dispatch rejects a pool that cannot satisfy them.

Old runtime pods may finish already accepted epochs with their pinned artifact versions. New Starts
do not route by a browser-selected version. Supporting simultaneous selectable connector versions
would require a future image-placement and lifecycle design.

## Contract Verification

- Instantiate every registered factory without connector I/O and validate one deterministic
  descriptor.
- Prove duplicate names, duplicate keys, overlapping prefixes, secret defaults, and ambiguous
  ownership fail inventory activation.
- Serialize each inventory repeatedly across JVM processes and prove identical revisions.
- Compare Catalog API descriptors with canonical validator and Coordinator inventories.
- Exercise every compatibility matrix row with retained Connection fixtures.
- Prove a stale page token cannot mix inventory revisions or tenant/policy scopes.
- Search responses and telemetry for class names, paths, Secret locators, and sentinel values.
