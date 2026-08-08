# ADR-040: Deployment-authoritative Connector Descriptor Catalog

## Status

Accepted

## Context

The Java `ConnectorRegistry` discovers deployment-bundled factories and currently exposes only
name, version, roles, and capabilities. Slice 19 needs richer descriptors for canonical validation
and forms, while Phase 6 also needs a tenant-facing connector inventory. If descriptors are entered
independently in PostgreSQL, uploaded by tenants, or maintained by the Console, the catalog can
advertise a connector or option contract that the compiler and runtime do not actually execute.

Connector rollout also needs stable evidence for pagination, validation caches, Connection schema
compatibility, and Coordinator/Worker version fencing. A connector name or semantic version alone
does not identify all of that behavior.

## Decision

Make the exact connector artifacts bundled with the deployed Java compiler/runtime the descriptor
content authority. Each registered factory publishes one immutable, deterministic, side-effect-free
descriptor with canonical name, artifact version, roles, capabilities, option ownership and
sensitivity, role-specific Connection requirements, and explicit accepted Connection schema
revisions.

Publish the complete inventory through an authenticated internal service. The Go API validates it
atomically against the configured execution profile and stores immutable snapshots for stable
tenant reads, retention, and audit correlation. Expose only read-only tenant-scoped List/Get Catalog
RPCs. Do not expose public descriptor mutation, connector binary upload, remote descriptor URLs, or
hot loading.

Identify canonical descriptor bytes with `descriptor_revision`; identify the sorted active set
with `inventory_revision`; and bind inventory, JobSpec schema, compiler build, and execution profile
into `compiler_revision`. Separately hash Connection-owned field semantics as
`connection_schema_revision`. A new connector artifact explicitly lists older Connection schema
revisions it can safely consume; compatibility is not inferred from semantic version.

Activate an inventory only when validator and every eligible runtime pool can satisfy it. Catalog
reads may use the last verified snapshot during publisher outage, but Create, Update, and Start
continue to fail closed when canonical validation/current compatibility cannot be established.
Coordinators retain the final artifact-version fence at execution.

## Consequences

- Catalog choices correspond to executable deployment artifacts rather than user-entered metadata.
- Descriptor-driven forms, validation, Connection compatibility, and runtime fencing share stable
  revision identities.
- Connector authors must provide deterministic schemas and classify every persisted option.
- PostgreSQL gains immutable catalog snapshots but cannot create executable connectors.
- Rolling deployment must coordinate validator and runtime inventories before activating catalog
  changes.
- Tenants cannot install custom connectors without a future isolated build, trust, placement, and
  lifecycle design.
- Historical descriptor retention supports diagnostics but does not make removed connectors
  selectable or executable.
