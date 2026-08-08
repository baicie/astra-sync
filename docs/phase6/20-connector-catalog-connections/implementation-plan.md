# Phase 6 Slice 20 Implementation Plan

## Status

Planned; implementation not started. Checkboxes are delivery gates, not completed work.

## Prerequisites

- [ ] Implement and verify Slice 18 OIDC, tenant context, permission registry, CSRF/TLS, service
  identity, and transactional audit foundations.
- [ ] Implement the Slice 19 protobuf-first canonical validator, mutation idempotency, and raw
  sensitive-option rejection required by this design.
- [ ] Keep Scheduler `connectionRef` rejection and all Connection/Console feature gates disabled
  until their explicit rollout stage.
- [ ] Record current connector artifact inventories and legacy Job/Connection CRD usage before
  applying schema or deployment changes.

## 1. Descriptor SPI and Protobuf Contracts (20A)

- [ ] Add versioned protobuf messages for connector descriptors, option definitions, Connection
  requirements, revisions, catalog list/get requests, and stable catalog errors.
- [ ] Extend Java `ConnectorDescriptor` with immutable display, role/mode, option schema,
  sensitivity/ownership, Connection schema, and compatibility metadata without connector I/O.
- [ ] Add builders or focused value types so descriptor construction remains readable and all
  invariants are enforced centrally.
- [ ] Update every CSV, JDBC, MySQL CDC, and PostgreSQL CDC factory with exact option ownership,
  bounds, sensitive fields, role requirements, and advanced-prefix policy.
- [ ] Reject raw control-plane persistence of `user`/`username`, `password`, token, key, and other
  descriptor-sensitive values; retain explicitly documented local-runner behavior outside the
  hosted control-plane boundary until separately migrated.
- [ ] Implement deterministic descriptor, inventory, Connection schema, and compiler revision
  calculation with cross-process fixtures.
- [ ] Generate Java/Go bindings and add compatibility checks for additive protobuf evolution.

## 2. Deployment Catalog (20B)

- [ ] Implement the authenticated Java inventory publisher from the exact `ServiceLoader`
  registry and compiler execution profile.
- [ ] Implement `control-plane/catalog` reconciliation, full-inventory validation, immutable
  PostgreSQL snapshots, atomic activation, retention, and last-verified read behavior.
- [ ] Add `ConnectorCatalogService` list/get methods, tenant authorization, bounded filtering,
  revision-bound page tokens, conditional responses, and redacted projections.
- [ ] Compare validator, API, Coordinator, and Worker inventory revisions during deployment and
  refuse activation/dispatch on unsupported skew.
- [ ] Add generated-method policy completeness tests and remove the provisional descriptor-listing
  ownership from the unimplemented Slice 19 validation service contract.
- [ ] Expose safe inventory health metrics and `connector.inventory.activate` service audit without
  class paths, service addresses, or descriptor internals.

## 3. Connection Domain and API (20C)

- [ ] Add PostgreSQL migrations for Connection identity/current state, immutable generations,
  restricted provider locators, Job bindings, execution bindings, tests, materialization receipts,
  idempotency results, tombstones, and required indexes/foreign keys.
- [ ] Enforce tenant/name uniqueness, positive versions/generations, immutable UIDs/connectors,
  append-only referenced generations, and reference-safe deletion in database/repository tests.
- [ ] Add protobuf-first `ConnectionService` messages and all ten RPCs with bounded requests,
  stable errors, deterministic JSON mapping, expected versions, and idempotency keys.
- [ ] Extend the Slice 18 permission vocabulary, built-in roles, method registry, direct API tests,
  transactional audit, and service-actor policy exactly as specified by the authorization matrix.
- [ ] Implement Create/Read/List/Update/Rotate/Enable/Disable/Delete state rules and redacted
  projections. Never call a provider inside a PostgreSQL mutation transaction.
- [ ] Keep Secret binding fields write only and use dedicated restricted persistence types rather
  than generic maps/serializers.
- [ ] Deprecate the YAML-only Connection CRD in deployment manifests or gate its installation; do
  not generate a controller from its historical schema.

## 4. Job Binding and Canonical Validation (20D)

- [ ] Resolve Job source/sink `connection_ref` by authenticated tenant and stable UID during Create
  and Update, requiring both Job permission and `connections.use`.
- [ ] Atomically replace `job_connection_binding` rows with the Job mutation and audit; ensure
  Delete removes bindings and same-name Connection recreation cannot capture a Job.
- [ ] Pass only descriptor-safe non-secret settings and secret-field presence into Java canonical
  validation; prove validator and API never receive provider locators or bytes.
- [ ] Reject Connection-owned/sensitive Job options, unknown pass-through keys, wrong connector,
  wrong role, disabled, unavailable, incompatible, inaccessible, and cross-tenant references.
- [ ] Extend Start to lock stable Connection UIDs, validate current state/revision, and atomically
  capture source/sink `execution_connection_binding` rows with the new epoch.
- [ ] Preserve desired-state no-op behavior without recapturing latest generations, and test
  Update/Rotate/Disable/Start races across API replicas.
- [ ] Keep Coordinator artifact and compiler revision fencing after admission validation.

## 5. Secret Provider and Scheduler Runtime (20E)

- [ ] Implement the bounded provider SPI with closeable byte buffers, exact logical fields,
  deadline/cancellation, safe receipts, and no list/write/delete operations.
- [ ] Implement `KUBERNETES_SECRET_V1` with server-owned tenant namespace mapping, tenant label,
  exact name/UID, `immutable: true`, key/size bounds, restricted service identity, and audit policy.
- [ ] Implement deterministic immutable epoch credential Secrets, strict runtime envelope, safe
  file mounts, configuration merge, connector-factory handoff, and in-memory cleanup.
- [ ] Extend Scheduler reconciliation to materialize only captured generations under a valid lease,
  recover uncertain creates, verify existing identity, block Coordinator launch on failure, and
  record redacted receipts/audit.
- [ ] Remove `jobDocument`'s non-empty `connectionRef` rejection only behind the runtime feature gate
  after all materialization tests pass; retain fail-closed behavior when the gate is off.
- [ ] Add terminal/cancellation cleanup and orphan sweeping that cross-check PostgreSQL before
  deleting only execution-scoped Secrets. Never delete external provider Secrets.
- [ ] Test provider outage/denial, Secret delete/recreate, lease loss, Scheduler crash at every
  boundary, pod retries, stale epochs, cleanup retry, and simultaneous source/sink generations.

## 6. Isolated Test and Console Workflows (20F)

- [ ] Add queued Connection test records, idempotent admission, expected version/generation capture,
  bounded status polling, expiry, and redacted stable result codes.
- [ ] Implement an isolated test executor with separate identity/namespace/queue, tenant egress
  policy, DNS pinning, strict resources/deadline, connector-owned read-only probes, and cleanup.
- [ ] Prove probes cannot accept caller SQL, enumerate data, mutate external state, follow unsafe
  redirects, reach metadata/control-plane addresses, or return vendor text.
- [ ] Add guarded Console catalog and Connection list/detail/create/edit/rotate/enable/disable/test/
  delete routes with descriptor-driven forms, write-only Secret references, explicit confirmation,
  optimistic conflict recovery, accessible states, and responsive no-overlap tests.
- [ ] Update Job editors to list only active role-compatible Connections and render stored
  unavailable references as blocking redacted issues.
- [ ] Test permission loss, tenant switching, CSRF, direct route tampering, stale descriptors,
  version conflicts, async timeout, and unknown outcomes in browsers and direct APIs.

## 7. Migration, Rollout, and Closeout (20G)

- [ ] Inventory all persisted Job options by active descriptor sensitivity and fail the tenant gate
  on unknown or raw sensitive keys.
- [ ] Publish a secure operator migration runbook that provisions immutable external Secrets,
  creates disabled Connections, updates Jobs with CAS, validates, enables, and verifies without
  automatic secret extraction or temporary plaintext files.
- [ ] Deploy additive schema, descriptor publisher, and read-only Catalog API first; compare catalog
  revisions across all eligible runtime images.
- [ ] Enable Connection CRUD for one staging tenant with runtime use off; reconcile resource,
  version, generation, idempotency, and audit counts.
- [ ] Enable shadow Job binding/validation, then isolated testing, then runtime materialization in
  staging with forced crash/rotation/cleanup exercises.
- [ ] Enable one production tenant and observe catalog skew, reference denials, rotation conflicts,
  materialization latency/failures, test budgets, cleanup backlog, audit failures, and secret
  sentinel scans before expanding.
- [ ] Record end-to-end runtime evidence, remove/deprecate the legacy CRD installation path, and
  mark Slice 20 Complete only after every verification gate passes.

## Rollback

Rollback disables Connection writes, tests, and new Starts requiring `connection_ref`; it does not
disable authentication or audit. Catalog reads can continue from the last verified snapshot.
Already accepted epochs keep their immutable Kubernetes credential Secret and can finish or follow
normal Stop. Scheduler cleanup and receipt reconciliation remain enabled until all epoch resources
are terminal.

Database migrations are additive through rollout. Do not drop Connection generations, bindings,
receipts, or tombstones while any binary or retained epoch references them. A failed connector
inventory rollout reactivates the prior compatible inventory and runtime image together; the API
must not independently relabel incompatible Connections as usable.

## Delivery Order

Implementation should land as reviewable sub-slices in order: `20A` descriptor contract, `20B`
catalog, `20C` Connection domain/API, `20D` Job binding, `20E` materialization, `20F` tests/Console,
and `20G` migration/production closeout. No earlier sub-slice may claim hosted credential safety or
remove the Scheduler rejection on its own.
