# ADR-058: Observability Catalog Backlog (Phase 17)

## Status

Accepted — **Superseded by Phase 25 closeout (2026-09-08).**

All six Phase 17 backlog metrics are **emitted** as of 2026-09-08.
ADR-058 remains on record as the design umbrella; the Phase 17
backlog is closed. Future metrics activation follows the same slice
pattern documented in this ADR.

## Context

`docs/observability/metrics-catalog.md` records the full Prometheus metric
inventory for the AstraSync control plane and data plane. As of the v0.3.0
release cut (ADR-057), seven of the catalog's Go-side business metrics
remain in a **descriptor-only / pending** state. The Phase 7 follow-up
section of `metrics-catalog.md` (the "Implementation status" table at the
top, and the follow-up paragraph near the end) explicitly lists the
remaining deferred items:

| Metric | Owner | Descriptor registered | Call site | Tests |
|---|---|---|---|---|
| `apiserver_sign_in_total` | api-server | F4 (`metrics.SignInTotal`) | none | descriptor + scrape only |
| `apiserver_session_revoke_total` | api-server | F4 (`metrics.SessionRevokeTotal`) | none | descriptor + scrape only |
| `auth_sign_in_total` | auth library | F4 (`authmetrics.AuthSignInTotal`) | none | descriptor + scrape only |
| `auth_session_revoke_total` | auth library | F4 (`authmetrics.AuthSessionRevokeTotal`) | none | descriptor + scrape only |
| `controller_job_state_total` | controller | **not registered** | **not registered** | none |
| `controller_epoch_fence_total` | controller | **not registered** | **not registered** | none |

ADR-047 §166 records the auth-library deferral ("sign-in and session-revoke
descriptors remain descriptor-only because those flows are owned by the
Console/auth boundary"). ADR-051 §130 records the OpenMetrics content
negotiation deferral. ADR-047 §166 also references the "remaining custom
Controller metrics" without a concrete owner.

The Phase 16 hygiene tooling (ADR-056) made it cheap to add a release cut
but did not close the observability backlog; nothing in the catalog points
an implementer at the specific files that need a call site, the label
allowlists they must respect, or the tests they must add. Three engineering
risks follow:

1. **No owner contract.** A new contributor who picks up
   `controller_epoch_fence_total` cannot tell from the catalog whether the
   Scheduler (which performs the actual fence), the Controller (which
   observes it through reconcile), or both should record it. The current
   ADR-030 / ADR-031 language does not pin the recorder owner.
2. **Label-cardinality drift.** Each pending metric carries
   `tenant_id` plus a second label. Without a concrete spec for the
   normalization helper, an implementer may either widen the allowlist
   (creating unbounded series) or skip normalization entirely (yielding
   raw caller input as label values). The auth-library / api-server
   metric packages already enforce canonical UUID + allowlist
   normalization; Controller does not.
3. **No replay path.** Auth flows that produce sign-in / session-revoke
   events today run in the Console. Wiring
   `apiserver_sign_in_total` requires either a new auth flow in the API
   Server or a Console forwarder. Neither decision has been recorded.

The catalog itself documents these rows as "pending"; the SLO handbook and
dashboard recipes both reference metrics that remain at zero series. New
on-call dashboards cannot surface a real burn rate for these families
until a call site exists.

## Decision

### 1. New phase: Phase 17 — Observability Backlog Activation

Phase 17 is the **explicit scope** for closing the catalog backlog. Each
pending metric receives its own slice with a defined owner, call site,
label contract, and test contract. ADR-058 is the **umbrella** decision;
each slice implements one or more rows.

Phase 17 follows the established slice pattern (Phase 6 / Phase 7 /
Phase 10 / Phase 13 / Phase 14 / Phase 15 / Phase 16 each introduce
multiple slices). A new `docs/phase17/README.md` records the slice list,
acceptance criteria, and out-of-scope items.

### 2. Owner and call-site allocation

The backlog resolves to three owners; each slice's PR MUST cite this
section.

| Metric | Recorder owner | Call site(s) | Reference |
|---|---|---|---|
| `apiserver_sign_in_total` | api-server `internal/metrics.SignInTotal` | new API Server sign-in handler introduced by Phase 17 slice 1 (or stub if deferred) | slice 1 |
| `apiserver_session_revoke_total` | api-server `internal/metrics.SessionRevokeTotal` | new API Server session-revoke RPC introduced by slice 1, or Console forwarder | slice 1 |
| `auth_sign_in_total` | `control-plane/auth/internal/authmetrics.AuthSignInTotal` | new sign-in helper exported from `control-plane/auth`, consumed by both the admin CLI bootstrap flows and the API Server sign-in handler when slice 1 introduces one | slice 2 |
| `auth_session_revoke_total` | `control-plane/auth/internal/authmetrics.AuthSessionRevokeTotal` | the auth-library revoke helper that already powers the admin CLI `revoke-session` operation; the admin CLI gains an observation at the success boundary only | slice 2 |
| `controller_job_state_total` | new `control-plane/controller/internal/metrics` registration | Controller reconcile boundary, at the durable state-transition commit (post-`repository.UpdateJobStatus`); `from_state` / `to_state` derived from before/after `repository.ReadJobStatus` snapshot | slice 3 |
| `controller_epoch_fence_total` | new `control-plane/controller/internal/metrics` registration | Controller reconcile boundary when a fence response indicates an obsolete execution epoch; `outcome` is `fenced` (success of the fence) or `failure` | slice 3 |

### 3. Label normalization rules

To prevent cardinality drift, every Go-side business metric recorder MUST
expose a `normalize<Label>` helper that enforces the existing allowlist
contract already present in the auth-library and API Server metric
packages:

- `normalizeTenant(value string) string`: returns `value` only when it
  parses as a canonical lowercase UUID and round-trips through
  `uuid.Parse`; otherwise returns `_unknown`. `_platform` is accepted as
  a fixed value for self-scope calls.
- `normalizeOutcome(value string) string`: returns the value only when
  it is in the metric's documented allowlist (for example,
  `success|rejected|failure` for auth flows, `success|fenced|failure`
  for epoch fences, `success|rejected|failure` for state transitions);
  otherwise returns `failure`.
- `normalizeWorkerID(value string) string`: trims whitespace and returns
  `_unknown` if empty or longer than 128 bytes; this matches the
  Scheduler's existing `scheduler_job_assignment_total` worker-id
  normalization.

These helpers are duplicated (not shared) across modules because the
existing `control-plane/auth/internal/authmetrics` package does not
export them. Phase 17 slice 0 (umbrella slice) introduces a small
`control-plane/observability/normalize` helper package that the auth,
api-server, controller, and scheduler metric packages can import. The
duplicated helpers in each package are deleted as each slice lands;
re-exports of `normalize` keep the public API of every package
unchanged.

### 4. Test contract

Each Phase 17 slice MUST add:

- A table-driven `*_test.go` case per call site covering the happy
  path (`success`), the rejection path (`rejected`), and the failure
  path (`failure`).
- A scrape-level test that exercises the owning long-running
  process's `/metrics` endpoint and asserts the metric appears with a
  non-zero sample after the call site fires. This mirrors the existing
  `metrics_test.go` scrape patterns for `AuthSignInTotal`,
  `SessionRevokeTotal`, `JobAssignmentTotal`, and `LeaseTakeoverTotal`.
- A label-allowlist assertion that feeding the call site with a
  non-canonical UUID (uppercase, braces, raw `123`) yields a series
  with `tenant_id="_unknown"` and NOT the raw input.

### 5. OpenMetrics content negotiation

ADR-051 §130 defers OpenMetrics content negotiation. Phase 17 does NOT
unblock that deferral; OpenMetrics remains a separate decision. The
`request_id` exemplar contract documented in ADR-047 §126 still requires
OpenMetrics negotiation before exemplars can transmit. Until then, all
new samples in Phase 17 are emitted as bounded time-series without
exemplars; the catalog records the deferred state explicitly.

### 6. CHANGELOG discipline

Each Phase 17 slice increments `CHANGELOG.md` under `## [Unreleased]`
following the established `[Unreleased]` rotation pattern. The
catalog row's status moves from "pending" to "emitted" once a slice
ships; the catalog itself never says "pending" without a back-reference
to the slice number that owns it.

## Consequences

### Positive

- Every descriptor in the catalog has a documented owner, a documented
  call site, and a documented normalization contract. Future
  contributors no longer need to read ADR-047 / ADR-051 to find out
  whether they can introduce a new sample.
- The label allowlist rules in §3 prevent the three cardinality
  regression modes already encountered in earlier slices (caller
  input as tenant, raw worker UUID, raw OIDC subject).
- Phase 17 delivers the **next** release cut's worth of work without
  re-touching ADR-047 or ADR-051; ADR-058 is additive.
- The new `control-plane/observability/normalize` package establishes a
  precedent for sharing label helpers across control-plane modules
  without depending on `control-plane/auth`.

### Negative

- Phase 17 slice 1 (API Server sign-in / session-revoke) introduces a
  new RPC surface that did not exist before. This is a feature change
  that the maintainer should sign off on as part of slice 1 review.
  The umbrella ADR cannot pre-approve slice 1; it scopes the work and
  records the contract.
- The `control-plane/observability/normalize` package is a new module.
  It introduces a cross-module import path that the existing
  `control-plane/auth` and `control-plane/api-server` packages do not
  currently share. The package is small (three helpers) but it is a
  precedent that future shared metric utilities will follow.
- ADR-058 freezes the recorder owner for `controller_job_state_total`
  and `controller_epoch_fence_total` as the Controller, not the
  Scheduler. If the Scheduler actually owns the durable state
  transition (the ADR-029 / ADR-030 architecture suggests this is
  possible), slice 3 must reconcile the recorder location with the
  Scheduler module's metric package. The umbrella ADR records the
  choice but does not pre-resolve it.

## Alternatives Considered

### Inline the work into Phase 16

Reject. Phase 16's scope (ADR-056) is "CI / Test Hygiene & Release
Tooling". Adding observability call sites would have widened Phase 16
beyond its non-goals (`docs/phase16/README.md` §27: "No application
code changes"). Phase 17 is a separate, scoped phase that respects the
phase discipline already in place.

### Create one giant slice that activates all six metrics at once

Reject. The metrics have different owners, different module boundaries,
and different label contracts. A single slice would be too large to
review and would conflate the auth-library / api-server / controller
decisions. Three slices (auth-flow, controller-state, controller-fence)
keep each PR focused.

### Defer until a customer asks

Reject. The catalog already promises the metrics. The SLO handbook
references them. The dashboard recipes reference them. Promising
metrics in the catalog and never emitting them creates on-call
ambiguity and a permanent smell. Closing the backlog is the right
default.

### Move the recorder to the Scheduler module for the Controller metrics

Reject for now. The Controller already owns the reconcile path
(ADR-031) and the Controller metric package already owns
`controller_job_controller_reconcile_duration_seconds`. Adding the two
missing counter families to the same module keeps the recorder owner
local. If the durable transition moves to the Scheduler in a future
slice, ADR-058 is the right place to update this decision.

## Implementation Plan (informational, not binding)

This section is informational. Each slice's PR carries the concrete
specification; this ADR only records the umbrella scope.

- **Phase 17 slice 0 — umbrella.** Create `docs/phase17/README.md`,
  introduce `control-plane/observability/normalize`, and add the
  umbrella slice's tests.
- **Phase 17 slice 1 — API Server sign-in / session-revoke.**
  Introduce the new RPCs (or forwarders) and wire the existing
  descriptors. Add the auth-flow slice tests.
- **Phase 17 slice 2 — Auth library observability.**
  Wire `AuthSignInTotal` and `AuthSessionRevokeTotal` into the admin
  CLI and any new auth helper. Add the auth-library slice tests.
- **Phase 17 slice 3 — Controller state and fence.**
  Register `controller_job_state_total` and `controller_epoch_fence_total`
  in `control-plane/controller/internal/metrics`, wire the call sites
  at the reconcile boundary, and add the controller slice tests.

The slice count and naming may evolve as Phase 17 progresses. This ADR
is the **decision**; `docs/phase17/README.md` is the **status**.

## References

- ADR-047 — Observability Handbook and Dashboard Consolidation
- ADR-051 — Java Data Plane Metrics Activation
- ADR-029 — Durable Control-Plane Job Lifecycle
- ADR-030 — Lease-Fenced Scheduler Dispatch
- ADR-031 — Controller Convergence and HA
- ADR-056 — CI / Test Hygiene & Release Tooling
- ADR-057 — v0.3.0 Release Cut (Phases 13-16)
