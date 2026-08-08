# Phase 6 Slice 19 Implementation Plan

## Design Gate

- [x] Inventory all eight JobService methods, current lifecycle states, expected-version behavior,
  desired-state idempotency, and Console read routes.
- [x] Define Create, Edit, Start/Restart, Stop, Delete, conflict, timeout, and lost-response flows.
- [x] Define canonical validation ownership without duplicating Java compiler rules in the browser
  or Go API.
- [x] Define permissions, CSRF, idempotency, audit, secret references, redaction, and dangerous
  operation controls.
- [x] Define implementation dependencies, feature gates, rollout, rollback, and acceptance criteria.

## Prerequisite Gate

- [ ] Implement and verify Slice 18 OIDC identity, tenant membership, API authorization, Console
  BFF session, TLS, and same-origin CSRF controls.
- [ ] Implement the transactional Job/audit unit of work and service-actor transition audit.
- [ ] Add a secret-safe public Job projection and migrate legacy raw sensitive connector options to
  pre-provisioned connection references.
- [ ] Keep `CONSOLE_JOB_MUTATIONS_ENABLED=false` until every prerequisite has production evidence.

## 1. Protobuf and Compatibility Contracts

- [ ] Add bounded idempotency/request correlation fields to all five Job mutation requests without
  renumbering or changing existing fields.
- [ ] Define `JobValidationService`, validation purpose/result/issues, connector descriptors, and
  stable error details protobuf-first.
- [ ] Add generated Go/Java types, JSON adapters, descriptor-policy mappings, and compatibility
  checks for old clients and stored JobSpecs.
- [ ] Require positive expected versions on authenticated Console Update, Start, Stop, and Delete
  paths while preserving documented exact-retry behavior.
- [ ] Add service-descriptor completeness tests for tenant scope and permission mappings.

## 2. Canonical Compiler Validation

- [ ] Add an internal Java compiler-validation service backed by strict JobSpec conversion,
  `ConnectorRegistry`, and `JobCompiler` under the deployment execution profile.
- [ ] Extend connector descriptors with bounded option schema, sensitivity, reference requirement,
  and side-effect-free validation metadata.
- [ ] Implement Go validation admission, deterministic spec digest, compiler revision, issue
  sanitization, timeout, concurrency limit, and bounded cache.
- [ ] Invoke canonical validation from Create, Update, and Start; prove Stop/Delete do not acquire
  compiler dependencies.
- [ ] Add parity fixtures covering every connector role, capability, delivery guarantee, transform,
  execution mode, option issue, and compiler revision change.

## 3. Mutation Unit of Work and Idempotency

- [ ] Add an idempotency table keyed by tenant, actor, full method, and key fingerprint, with request
  digest, in-progress ownership, bounded result, audit event ID, and expiry.
- [ ] Refactor Job mutation repositories so Job state, idempotency completion/tombstone, and required
  audit event commit atomically.
- [ ] Implement exact replay, mismatched-digest rejection, concurrent-key ownership, abandoned
  in-progress lease recovery, deterministic-error completion, transient-error release, and
  retention cleanup.
- [ ] Return typed conflict, lifecycle, validation, and retry details without resource disclosure or
  secret values.
- [ ] Preserve desired-state no-op and epoch behavior with race tests across API replicas.

## 4. Console BFF Write Boundary

- [ ] Add mutation and validation client interfaces without weakening the current read adapter.
- [ ] Register write routes only when the production prerequisite gate passes; require session,
  tenant, CSRF, content type, body limit, request ID, idempotency key, and deadline.
- [ ] Ignore browser namespaces, map selected tenant and expected version into protobuf requests,
  and return canonical protobuf JSON plus stable error details.
- [ ] Propagate access tokens only server-to-server and prevent credentials, specs, and references
  from access logs or error bodies.
- [ ] Test route absence while disabled, cross-tenant tampering, method/content-type confusion,
  CSRF failures, body limits, cancellation, and upstream error mapping.

## 5. Console Workflows

- [ ] Add descriptor-driven Create and Edit routes with stable form layout, field validation,
  canonical issue summary, dirty-draft guard, and immutable identity fields.
- [ ] Add permission- and lifecycle-aware action controls to Job detail with Start/Restart, Stop,
  Edit, and Delete confirmations.
- [ ] Add one client operation controller for validation, submission, accepted desired state,
  convergence polling, terminal status, timeout, and unknown outcomes.
- [ ] Refresh full Job snapshots before action confirmation and after status transitions; never use
  status-only polling as the expected-version source.
- [ ] Preserve drafts through validation and version conflicts; implement explicit reload, discard,
  and reapply without automatic overwrite.
- [ ] Add typed-name Delete confirmation and ensure active failures offer Stop rather than force
  delete.
- [ ] Add keyboard, focus, screen-reader status, mobile/desktop layout, and no-overlap browser tests.

## 6. Security and Operational Verification

- [ ] Verify each built-in role and all eight JobService mappings through direct API and Console
  tampering tests.
- [ ] Verify audit atomicity, no-op events, idempotency correlation, denied attempts, service-actor
  causation, and database rollback on audit failure.
- [ ] Run redaction sentinels through browser responses, compiler issues, logs, traces, audit rows,
  idempotency rows, and diagnostics.
- [ ] Run PostgreSQL races for stale versions, same/different idempotency digests, lost responses,
  delete tombstones, concurrent Start/Stop, and multiple API replicas.
- [ ] Run browser end-to-end tests for every lifecycle state, permission change, validation error,
  conflict, dependency outage, accepted timeout, and recovery path.
- [ ] Prove write controls remain disabled when status changes but a current full Job version cannot
  be loaded.
- [ ] Verify compiler outage isolation, bounded admission, cache invalidation, connector inventory
  mismatch, and Coordinator compile fencing.

## 7. Rollout and Closeout

- [ ] Deploy additive schema and validation service with Console writes disabled.
- [ ] Run shadow validation against existing Jobs and block tenants with raw sensitive options or
  compiler incompatibilities.
- [ ] Enable staging writes, reconcile Job/idempotency/audit counts, and exercise rollback without
  disabling authentication.
- [ ] Enable one production tenant, observe mutation latency, conflict/no-op rates, compiler errors,
  audit failures, and convergence timeouts, then expand deliberately.
- [ ] Record runtime evidence, mark Slice 19 Complete only after all gates pass, and keep deferred
  features out of the completion claim.

## Delivery Slices

Implementation should be delivered as reviewable sub-slices in this order:

1. 19A: protobuf validation/idempotency contracts and descriptor metadata.
2. 19B: Java canonical compiler-validation service and Go admission adapter.
3. 19C: transactional Job/idempotency/audit mutation unit of work.
4. 19D: guarded Console BFF write routes and error mapping.
5. 19E: operator workflows, browser verification, rollout, and closeout.

Validation-only infrastructure can begin while Slice 18 is being implemented. No sub-slice may
register production Console mutation routes before the prerequisite gate is complete.
