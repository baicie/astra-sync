# ADR-060: Phase 18 Scheduler Metrics Normalize (Slice 44)

## Status

Accepted

## Context

Phase 17 (ADR-058) activated three of the four metric Recorder owners
in the control plane:

- api-server (slice 43.1) — six families routed through
  `control-plane/observability/normalize`;
- auth library (slice 43.2) — two families (auth_sign_in_total +
  auth_session_revoke_total) routed through `normalize`;
- controller (slice 43.3) — three families
  (controller_job_controller_reconcile_duration_seconds,
  controller_job_state_total, controller_epoch_fence_total)
  routed through `normalize` (the controller also deleted its
  package-local `normalizeTenant` / `normalizeOutcome` helpers in
  favour of the shared package).

The fourth control-plane Recorder owner — the Scheduler
(`control-plane/scheduler/internal/metrics`) — was not touched by
Phase 17. Its current shape is **package-level `promauto`
CounterVec / HistogramVec**, registered against the global default
registry, with three families:

- `scheduler_job_assignment_total` (counter; tenant_id,
  worker_id, outcome);
- `scheduler_lease_takeover_total` (counter; tenant_id, outcome);
- `scheduler_job_reconcile_duration_seconds` (histogram; tenant_id).

These families are emitted today by the Scheduler long-running
process — `scripts/release-dry-run.py` and the catalog report
status "emitted by the Scheduler" — but the **label normalisation**
contract is not enforced by the recorder. The package-level
`promauto` CounterVec accepts any caller input as a label value, so
a non-canonical tenant UUID (`ALICE@acme.example`), an
underspecified worker id (`""`), or an empty outcome collapses into
a new unbounded series. ADR-058 §3 records the canonical
labelling rules (canonical-lowercase-UUID tenant, bounded outcome
allowlist, length-bounded worker id) as the contract every metric
owner MUST enforce; the Scheduler metric package is the last
control-plane Recorder owner that does not enforce it.

The catalog reports three families under the Scheduler, but
`docs/observability/metrics-catalog.md` does not list the
Scheduler's row status as "Pending" or "Recorder wired" — it is
listed as "emitted by the Scheduler". That label is correct in
spirit (the families do appear in scrape output) but it understates
the gap: the existing package-level `promauto` surface does not
satisfy ADR-058 §3. A future contributor who adds a fourth family
(`scheduler_worker_id_observed_total`, say) cannot copy the
existing pattern without duplicating helpers; that is the exact
failure mode ADR-058 §3 was written to prevent.

The Scheduler module already has go.mod and CI wiring
(`scripts/run-go-modules.py` lists `control-plane/scheduler`).
Adding `io.astrasync/control-plane/observability` as a require +
replace is mechanical, identical to the slice-43.0 / 43.1 / 43.2 /
43.3 pattern.

## Decision

### 1. Phase 18 scope: scheduler metrics Recorder + normalize

Phase 18 is the umbrella for closing the Scheduler metric package
gap. It follows the Phase 17 slice template (44.0 umbrella +
observability dep; 44.1 Recorder + Observe methods; 44.2
table-driven tests).

Out of scope for Phase 18:

- The multi-region replication Recorder
  (`control-plane/replication/metrics`). Its labels are
  `target_region`, `peer_region`, `event_type`, and `outcome` —
  none of them canonical-lowercase-UUID tenant or bounded worker
  id. The replication Recorder would benefit from a separate
  label-allowlist helper (`normalizeLabel`) but routing through
  `control-plane/observability/normalize` would be a misuse of
  the `NormalizeTenant` / `NormalizeOutcome` /
  `NormalizeWorkerID` helpers; a future ADR can decide whether
  replication needs an `observability/normalize.FreeText` style
  helper or keeps its internal `normalizeLabel`.
- The connection-test executor Recorder
  (`control-plane/scheduler/internal/connectiontestmetrics`).
  It already has its own Recorder + internal `Observe` collapse,
  and its label set (`tenant_id`, `outcome`) is a subset of the
  Scheduler families. Migrating it to the shared `normalize`
  package is a small mechanical follow-up that fits naturally as
  slice 44.3 if Phase 18 has scope room after slice 44.1 lands;
  the slice is optional because the connection-test Recorder
  already collapses to `_unknown` for empty tenant and to
  `failure` for non-allowlisted outcome (the contract is
  enforced; only the helper source is local).
- Any change to the Java data-plane emission (26.F8 follow-up).
  That follow-up owns worker protocol changes and Java trust
  binding; it is the natural Phase 19 candidate.

### 2. Slice 44.0: umbrella + Scheduler `go.mod` observability dep

A new umbrella ADR (this ADR) records the Phase 18 scope. The
Scheduler module's `go.mod` gains `require
io.astrasync/control-plane/observability v0.0.0-00010101000000-000000000000`
and `replace io.astrasync/control-plane/observability => ../observability`,
identical to the slice-43.0/43.1/43.2/43.3 pattern.

`scripts/run-go-modules.py` already lists
`control-plane/scheduler`; no script change is required.

### 3. Slice 44.1: Scheduler Recorder + Observe methods

The Scheduler metric package
(`control-plane/scheduler/internal/metrics`) gains a Recorder
struct that owns dedicated CounterVec / HistogramVec families
registered against an injected `prometheus.Registerer`. Three new
methods route every label value through `observability/normalize`:

- `ObserveAssignment(tenantID, workerID, outcome string)` —
  routes `tenant_id` through `NormalizeTenant`,
  `worker_id` through `NormalizeWorkerID`, and `outcome`
  through `NormalizeOutcome` with allowlist
  `success | rejected | failure`.
- `ObserveLeaseTakeover(tenantID, outcome string)` —
  routes `tenant_id` through `NormalizeTenant` and `outcome`
  through `NormalizeOutcome` with allowlist `success` (only
  `success` is documented for `scheduler_lease_takeover_total`
  per `docs/observability/metrics-catalog.md`; the allowlist is
  intentionally tighter than the auth outcome allowlist).
- `ObserveReconcile(tenantID string, duration time.Duration)` —
  routes `tenant_id` through `NormalizeTenant`; the duration
  value is recorded verbatim (the histogram is a duration, not
  a label-derived value).

The package-level `JobAssignmentTotal` /
`LeaseTakeoverTotal` / `JobReconcileDuration` `promauto` Vecs
remain registered against the default registry for backward
compatibility (slice 43.2 already established this "Recorder is
strictly additive" pattern). New call sites in the Scheduler
long-running consumer go through the Recorder; legacy import paths
(including the slice-44.2 tests' package-level
`JobAssignmentTotal.WithLabelValues(...).Inc()` invocation) keep
working without behaviour change.

The Recorder exposes a `HandlerFor(gatherer prometheus.Gatherer)`
entry point mirroring the slice-43.2 design so a long-running
Scheduler consumer can host the Recorder-owned registry from its
own `/metrics` endpoint without competing for the global default
registry.

### 4. Slice 44.2: test contract

Slice 44.2 lands the same test contract ADR-058 §4 requires of
Phase 17:

- table-driven test per call site covering the happy path
  (`success`), the rejection path (`rejected`), and the failure
  path (`failure`);
- scrape-level test that exercises the Scheduler long-running
  consumer's `/metrics` endpoint and asserts the metric appears
  with a non-zero sample after the call site fires;
- label-allowlist assertion that feeding the call site with a
  non-canonical UUID (uppercase, braces, raw e-mail) yields a
  series with `tenant_id="_unknown"` and NOT the raw input;
- cross-call cardinality bound test asserting that N distinct
  caller inputs across both families produce only M bounded
  series;
- backward-compatibility assertion that the package-level
  `JobAssignmentTotal` / `LeaseTakeoverTotal` /
  `JobReconcileDuration` Vecs remain registered against the
  default registry.

### 5. Slice 44.3 (optional): connection-test Recorder migrate

If Phase 18 has scope room after 44.2 lands, slice 44.3 replaces
`connectiontestmetrics`' internal `Observe` collapse with calls
through `observability/normalize`. The contract does not change
(today's internal collapse already satisfies ADR-058 §3); only
the helper source moves. Slice 44.3 is a strict refactor; no
new label values appear and no test assertion changes.

If scope is tight, slice 44.3 is dropped from Phase 18; the
connection-test Recorder can land as a Phase 19 follow-up.

### 6. Acceptance criteria + release cut

Phase 18 closes once 44.0 + 44.1 + 44.2 land and all acceptance
criteria are Done. Slice 44.3 (optional) is a separate Done
flag. Phase 18 ships as `v0.5.0` in the next release cut;
ADR-061 (v0.5.0 release cut) will follow the ADR-057 / ADR-059
template.

## Consequences

### Positive

- Every control-plane Recorder owner (api-server, auth, controller,
  Scheduler) routes label values through the same
  `observability/normalize` package. The ADR-058 §3
  "duplicated helpers are deleted as each slice lands" invariant
  is now fully satisfied across the Go control plane.
- The Scheduler Recorder follows the slice-43.2 design pattern
  (Recorder + `HandlerFor`), so future contributors who add a
  fourth Scheduler family can copy the contract.
- The Scheduler emit rows in
  `docs/observability/metrics-catalog.md` are honest: the row
  status moves from "emitted by the Scheduler" (which understates
  the helper-duplication risk) to "Recorder wired, slice 44"
  (which is the same status language Phase 17 adopted).
- Slice 44.3 (if landed) eliminates the last duplicated
  `normalizeLabel`-style helper in the Go control plane.

### Negative

- The Scheduler metric package is the first Phase 18 / slice 44
  candidate and there is no live production sample to compare
  against; the test contract must rely on synthetic inputs, not
  on real Scheduler traffic. This is acceptable because the
  Recorder contract is the same as Phase 17's, but it is a
  limitation worth recording.
- Replication metrics and connection-test metrics keep their
  internal `normalizeLabel` / `Observe` helpers until a follow-up
  ADR can decide whether region / event_type labels should route
  through `normalize` or use a new `FreeText` helper. The cost is
  small (each Recorder has its own internal helper) but it
  breaks the "shared package everywhere" rule.
- The Recorder + `HandlerFor` design means future Scheduler
  consumers must choose between the package-level
  `JobAssignmentTotal` (registered against the default registry)
  and the Recorder-owned registry. They are not interchangeable
  — the package-level Vec is for backward compatibility and the
  Recorder is for new code. This is the same trade-off as
  slice-43.2; documenting it here so the next contributor does
  not have to re-derive the rule.

## Alternatives Considered

### Skip Phase 18 and ship Scheduler Recorder as a one-off slice

Reject. The Scheduler Recorder is the natural follow-up of Phase
17; treating it as a one-off would leave the ADR-058 §3
"duplicated helpers are deleted" invariant partially satisfied.
A one-off slice also lacks an umbrella ADR, which is the
established Phase 17 / Phase 18 discipline.

### Phase 18 = "all emission call site sub-slices (43.1.5, 43.2.5, 43.3.5)"

Reject. Those sub-slices have open ownership questions
(API Server sign-in handler needs new RPC + RBAC role — AGENTS.md
§8 decision gate; controller reconcile path needs ADR-030/031
sign-off; admin CLI Recorder needs the long-running consumer
question resolved). They are not a single coherent Phase.

### Phase 18 = multi-region replication Recorder normalize

Reject. The replication Recorder labels
(`target_region`, `peer_region`, `event_type`) are not canonical
lowercase UUIDs and not bounded worker ids. Routing them through
`observability/normalize` would be a misuse; a separate label
helper (`FreeText` with a length cap) is the correct design but
out of scope here. Recording this as a Phase 19 candidate keeps
the replication Recorder unchanged.

### Phase 18 = Java data-plane emission follow-up (26.F9)

Reject. That follow-up touches the worker protocol and Java
trust binding, which are large architectural decisions. The
ADR-051 §7 follow-up note already records it as out-of-scope for
any current slice; a Phase 19 ADR can pick it up once the worker
protocol decision is made.
