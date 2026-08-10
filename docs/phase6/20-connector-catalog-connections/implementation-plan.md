# Phase 6 Slice 20 Implementation Plan

## Status

Implementation complete. Repository and CI verification gates are delivered; production rollout
and tenant enablement remain explicit operator actions. All runtime-affecting gates default closed.

## Prerequisites

- [x] Deliver Slice 18 OIDC, tenant context, permission registry, Console session/CSRF boundary,
  service identity, and transactional audit foundations.
- [x] Deliver Slice 19 protobuf-first canonical validation, CAS/idempotent Job mutations, and raw
  sensitive-option rejection.
- [x] Preserve fail-closed Connection, test, and runtime behavior until an operator enables each
  rollout gate.
- [x] Capture the deployment connector inventory and deprecate the historical unmanaged
  Connection CRD.

## 1. Descriptor SPI and Protobuf Contracts (20A)

- [x] Add versioned descriptor, inventory, catalog, Connection, test, and stable error protobuf
  contracts with generated Java and Go bindings.
- [x] Extend Java `ConnectorDescriptor` with immutable display metadata, role/mode, option schema,
  ownership, sensitivity, Connection requirements, and compatibility metadata.
- [x] Centralize descriptor invariants in focused immutable value types and builders.
- [x] Describe CSV, JDBC, MySQL CDC, and PostgreSQL CDC option ownership, bounds, sensitivity,
  role requirements, and advanced prefixes.
- [x] Reject sensitive, Connection-owned, and unknown options from hosted Job persistence while
  retaining the explicitly separate local-runner boundary.
- [x] Calculate deterministic descriptor, inventory, Connection schema, and compiler revisions and
  verify generated protocol determinism.

## 2. Deployment Catalog (20B)

- [x] Export the exact `ServiceLoader` connector inventory from the Java compiler profile.
- [x] Validate and atomically activate immutable PostgreSQL inventory/descriptor snapshots with
  retained reads and transactional activation audit.
- [x] Implement authorized `ConnectorCatalogService` list/get RPCs with bounded filters,
  revision-bound page tokens, conditional reads, and redacted projections.
- [x] Fence compiler, API admission, Coordinator, Worker, and deployment catalog revisions and
  provide deterministic `catalog-check` evidence.
- [x] Validate deny-by-default generated-method policy coverage and catalog reconciliation
  readiness without exposing class paths, service addresses, or descriptor internals.

## 3. Connection Domain and API (20C)

- [x] Add migrations for Connection state, immutable generations, restricted locators, Job and
  execution bindings, tests, receipts, cleanup obligations, idempotency, audit, and tombstones.
- [x] Enforce tenant/name uniqueness, positive versions/generations, immutable UID/connector,
  append-only referenced generations, and reference-safe deletion in repositories and tests.
- [x] Implement all ten protobuf-first Connection RPCs with bounded requests, stable errors,
  deterministic projection, CAS versions, and idempotency keys.
- [x] Extend roles, permissions, method registry, direct API authorization, and transactional audit
  according to the authorization matrix.
- [x] Implement Create/Read/List/Update/Rotate/Enable/Disable/Delete lifecycle rules without a
  provider call inside a PostgreSQL mutation transaction.
- [x] Keep provider locators write-only at public boundaries and represent them with restricted
  typed persistence models rather than generic request maps.
- [x] Mark the historical YAML-only Connection CRD deprecated without creating a controller from
  its incompatible schema.

## 4. Job Binding and Canonical Validation (20D)

- [x] Resolve source/sink `connection_ref` in the authenticated tenant and require both Job
  permission and `connections.use`.
- [x] Atomically replace stable Job UID bindings with each Job mutation and audit event; same-name
  Connection recreation cannot capture an existing Job.
- [x] Pass only descriptor-safe settings and secret-field presence into canonical Java validation;
  provider locators and bytes never cross that boundary.
- [x] Reject sensitive/Connection-owned/unknown options and unavailable, incompatible,
  wrong-connector, wrong-role, inaccessible, or cross-tenant references.
- [x] Lock current stable Connection UIDs during Start and capture immutable source/sink generation
  rows in the same epoch transaction.
- [x] Preserve desired-state no-op semantics and generation fencing under Update, Rotate, Disable,
  and Start races.
- [x] Keep Coordinator and Worker compiler/artifact revision fencing after admission.

## 5. Secret Provider and Scheduler Runtime (20E)

- [x] Implement a bounded provider SPI with closeable/zeroed byte buffers, exact logical fields,
  cancellation/deadlines, safe receipts, and no list/write/delete methods.
- [x] Implement `KUBERNETES_SECRET_V1` with server-owned tenant namespace, exact name and UID,
  tenant label, `immutable: true`, exact key set, and byte bounds.
- [x] Create deterministic immutable epoch credential Secrets and strict runtime envelopes mounted
  as read-only files and consumed by Java runtime bootstrap.
- [x] Materialize only captured generations under a valid dispatch lease, recover uncertain
  creates, verify identity, and commit redacted receipts before Coordinator launch.
- [x] Allow non-empty `connectionRef` only behind the API runtime and Scheduler materialization
  gates; default behavior remains fail-closed.
- [x] Clean terminal/canceled execution Secrets and reconcile only validated AstraSync-owned orphan
  resources; external provider Secrets are never deleted.
- [x] Cover provider denial/outage, Secret UID recreation, lease loss, retries, source/sink
  generations, receipt conflicts, and cleanup behavior.

## 6. Isolated Test and Console Workflows (20F)

- [x] Add queued, generation-captured, idempotent Connection tests with bounded admission, polling,
  expiry, and redacted result codes.
- [x] Implement the isolated executor with a separate identity/namespace/queue, resource quota,
  NetworkPolicy, DNS pinning, egress policy, and strict deadlines.
- [x] Restrict probes to connector-owned read-only transport/TLS/authentication handshakes with no
  caller SQL, data enumeration, arbitrary redirects, metadata access, or vendor text response.
- [x] Deliver authenticated Console catalog, Connection lifecycle/test, and Job editor workflows
  with descriptor-driven forms, write-only Secret replacement, confirmation, and CAS recovery.
- [x] List only active role-compatible Connections while preserving unavailable stored references
  as blocking redacted validation issues.
- [x] Cover permission/tenant/CSRF tampering, stale descriptors, conflicts, async timeout, direct
  API routes, and responsive Console behavior in automated tests.

## 7. Migration, Rollout, and Closeout (20G)

- [x] Provide value-free Job JSONB option inventory SQL and fail-closed descriptor comparison rules.
- [x] Publish the secure operator [migration and rollback runbook](migration-and-rollback.md),
  including immutable provider provisioning, disabled Connection staging, CAS Job migration,
  validation, rollout observation, and gate-first rollback.
- [x] Package additive schema, inventory publication, read-only Catalog, default-closed Helm values,
  invalid half-enabled configuration checks, images, and CI gates.
- [x] Record unit, PostgreSQL, protocol, catalog, Helm, Linux build, container, redaction, and Console
  evidence in [verification.md](verification.md).
- [ ] Operator: enable Connection administration for one staging tenant and reconcile resource,
  generation, idempotency, and audit counts.
- [ ] Operator: enable isolated testing and then runtime materialization in staging; exercise forced
  crash, rotation, Stop, retry, and cleanup procedures.
- [ ] Operator: enable one production tenant, observe the runbook signals through the acceptance
  window, and expand only after an explicit change review.

The unchecked items require access to a real staging/production deployment and are deliberately
not represented as repository implementation work. They do not weaken default behavior: the
shipped chart keeps every Connection write, test, and runtime gate disabled.

## Rollback Contract

Rollback disables new Connection mutations, test admission, and Connection-backed Starts while
keeping authentication, audit, Catalog reads, and Connection reads available. Scheduler
materialization stays enabled until already accepted epochs finish or stop and every generated
credential Secret and cleanup obligation is reconciled. Migrations remain additive; generations,
bindings, receipts, tombstones, idempotency records, and audit events are retained while referenced.

A failed inventory rollout reactivates the prior compiler/API/Coordinator/Worker-compatible image
set together. The API never independently relabels incompatible Connections as usable.
