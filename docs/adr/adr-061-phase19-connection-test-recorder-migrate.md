# ADR-061: Phase 19 Connection-Test Recorder Migrate (Slice 45)

## Status

Accepted

## Context

ADR-060 §5 recorded slice 44.3 (connection-test Recorder migrate
from internal `Observe` collapse to `observability/normalize`) as
**optional** scope for Phase 18. Phase 18 shipped with 12/12
mandatory acceptance criteria Done and did not exercise that
option, so slice 44.3 is the natural Phase 19 candidate.

The current `connectiontestmetrics` package
(`control-plane/scheduler/internal/connectiontestmetrics`) is
already a Recorder-shaped design — it owns a `Recorder` struct
and an `Observe(tenantID, outcome string)` method — but its
normalization logic is duplicated locally:

```go
// control-plane/scheduler/internal/connectiontestmetrics/metrics.go
func (r *Recorder) Observe(tenantID, outcome string) {
    if r == nil || r.ConnectionTestTotal == nil {
        return
    }
    tenantID = strings.TrimSpace(tenantID)
    if tenantID == "" {
        tenantID = "_unknown"
    }
    switch outcome {
    case OutcomeSuccess, OutcomeRejected, OutcomeFailure:
    default:
        outcome = OutcomeFailure
    }
    r.ConnectionTestTotal.WithLabelValues(tenantID, outcome).Inc()
}
```

The contract — empty / whitespace tenant collapses to `_unknown`;
non-allowlisted outcome collapses to `failure` — matches
ADR-058 §3 exactly. The only thing that does **not** match is
the helper source: connection-test uses its own collapse rather
than routing through
`io.astrasync/control-plane/observability/normalize.NormalizeTenant`
/ `NormalizeOutcome`.

Three Recorder owners in the control plane now route through
`observability/normalize`:

- api-server (Phase 17 slice 43.1)
- auth library (Phase 17 slice 43.2)
- controller (Phase 17 slice 43.3)
- scheduler (Phase 18 slice 44.1)

The connection-test Recorder is the **fifth** control-plane
Recorder owner, and the **only** one that still has a duplicated
helper. ADR-058 §3's "duplicated helpers are deleted as each
slice lands" invariant is therefore not yet fully satisfied.

In addition, the connection-test Recorder's outcome allowlist
(`success | rejected | failure`) differs from the Scheduler
Recorder's lease-takeover allowlist (`success` only) and from the
auth library's sign-in allowlist (`success | rejected |
failure`). The allowlist itself is correct — it matches the
catalog row for `connection_test_total` — but it currently lives
as an untyped `switch` rather than as a documented slice
constant that other code can reuse.

The connection-test long-running consumer
(`scheduler/cmd/connection-test-executor`) is registered against
the Scheduler module's Go module
(`io.astrasync/control-plane/scheduler`) and therefore already
has access to the `observability/normalize` import path that
Phase 18 slice 44.0 wired into the Scheduler `go.mod`. No
`go.mod` change is required for Phase 19.

## Decision

### 1. Phase 19 scope: connection-test Recorder migrates to shared `normalize`

Phase 19 is the umbrella for closing the last duplicated
normalize helper in the control plane. It follows the Phase 18
slice template:

- slice 45.0 (umbrella + `go.mod` check) — recorded in this ADR.
  No code change; the Scheduler module already requires
  `observability` after Phase 18 slice 44.0.
- slice 45.1 (Recorder migrate) — replace the
  `connectiontestmetrics.Observe` internal collapse with calls
  through `NormalizeTenant` + `NormalizeOutcome`. The Recorder
  contract does **not** change: same allowlist, same
  `_unknown` fallback, same nil-receiver no-op semantics.
- slice 45.2 (test contract) — table-driven tests for happy /
  rejected / failure canonical paths, `_platform` self-scope,
  non-canonical UUID collapse, outcome allowlist collapse,
  cross-call cardinality bound, and a backward-compatibility
  assertion that the package-level
  `connectiontestmetrics.ConnectionTestTotal` `promauto` Vec
  (registered against the default registry) continues to expose
  the same series.

The connection-test Recorder is **strictly additive** under
Phase 19: the package-level `ConnectionTestTotal` `promauto`
Vec is preserved, mirroring slice 44.1 / slice 43.2 / slice
43.1 design discipline. New call sites in the Connection Test
Executor long-running consumer route through the Recorder;
legacy imports continue to work.

Out of scope for Phase 19:

- **Replication Recorder normalize** — same reason as ADR-060
  §1: the replication Recorder labels
  (`target_region`, `peer_region`, `event_type`) are not
  canonical-lowercase-UUID tenant or bounded worker id, and
  routing them through `NormalizeTenant` / `NormalizeWorkerID`
  would be a misuse. The replication Recorder continues to use
  its internal `normalizeLabel`. A future ADR (Phase 20
  candidate) can decide whether to introduce a
  `observability/normalize.FreeText` helper.
- **Java data-plane emission follow-up (`26.F9`)** — same
  reason as ADR-060 §1: worker-protocol trust binding is a
  large architectural decision; ADR-051 §7 already records it
  as a Phase 19 candidate but only because Phase 19 is the
  natural next phase name. It is NOT a Phase 19 scope item.
  This ADR makes that explicit: Phase 19 is the
  connection-test Recorder migrate, not the Java data-plane
  emission follow-up.
- **Emission sub-slices (43.1.5 / 43.2.5 / 43.3.5)** — same
  reason as Phase 18: those sub-slices have open ownership
  questions (API Server sign-in handler needs new RPC + RBAC
  role — AGENTS.md §8 decision gate; controller reconcile path
  needs durable commit decision; admin CLI Recorder needs the
  long-running consumer question resolved). They are not a
  single coherent Phase and are not in scope here.
- **Phase 19 + 26.F9 dual-track** — this ADR picks **one**
  theme per Phase. 26.F9 is documented for Phase 20+, not
  Phase 19.

### 2. Slice 45.1: Recorder migrate

The migration is a strict refactor:

- Replace the local `tenantID = strings.TrimSpace(tenantID); if
  tenantID == "" { tenantID = "_unknown" }` collapse with a
  single call to
  `normalize.NormalizeTenant(tenantID)`. The observable
  behavior is identical: the connection-test Recorder contract
  for empty / whitespace tenant has always been "collapse to
  `_unknown`", and `NormalizeTenant` does the same.
- Replace the local `switch outcome { case
  OutcomeSuccess, OutcomeRejected, OutcomeFailure: default:
  outcome = OutcomeFailure }` collapse with a single call to
  `normalize.NormalizeOutcome(outcome,
  []string{OutcomeSuccess, OutcomeRejected, OutcomeFailure},
  OutcomeFailure)`. The default value (`failure`) is preserved
  as the third argument; the allowlist is encoded in
  `outcomeAllowlist` as a package-private slice so future
  contributors can reuse it (the slice is also the source of
  truth for `OutcomeSuccess` / `OutcomeRejected` /
  `OutcomeFailure` constants).
- Keep the nil-receiver guard (defensive check that the Recorder
  is constructed; matches every other Recorder owner).
- Keep the package-level `ConnectionTestTotal` `promauto`
  CounterVec registered against the default registry so the
  legacy `Handler()` entry point and the pre-slice-45
  `metrics_test.go` continue to scrape the same series.

The `DefaultRecorder()` constructor stays unchanged: it returns
a Recorder backed by the package-level `ConnectionTestTotal`
Vec. New call sites can construct a Recorder with
`NewRecorder(registerer)` against an injected
`prometheus.Registerer` (e.g. a long-running consumer that
hosts the Recorder-owned registry from its own `/metrics`
endpoint); legacy import paths keep working with
`DefaultRecorder()`.

### 3. Slice 45.2: test contract

The slice 45.2 test contract mirrors Phase 17 / Phase 18:

- table-driven test covering the canonical success / rejected /
  failure outcomes;
- `_platform` self-scope assertion (matches Phase 17 / 18);
- non-canonical UUID collapse (uppercase, e-mail, braced
  UUID);
- empty / whitespace outcome collapse (matches the existing
  contract);
- cross-call cardinality bound test asserting that N distinct
  caller inputs across the family produce only M bounded
  series;
- nil-receiver safety test;
- nil-registerer rejection test (sentinel `ErrNilRegisterer`,
  matching Phase 18 design);
- duplicate-registration rejection test (sentinel
  `ErrDuplicateMetric`, matching Phase 18 design);
- backward-compatibility test confirming the package-level
  `ConnectionTestTotal` Vec remains registered against the
  default registry and continues to expose the same series via
  `Handler()`.

### 4. Acceptance criteria + release cut

Phase 19 closes once 45.0 + 45.1 + 45.2 land and all acceptance
criteria are Done. Phase 19 ships as `v0.5.0` in the next release
cut. ADR-061 is the umbrella; a future ADR (Phase 19 release
cut, modelled after ADR-057 / ADR-059) records the versioned
section.

## Consequences

### Positive

- Every control-plane Recorder owner (api-server, auth,
  controller, Scheduler, Connection Test Executor) routes
  label values through the same
  `observability/normalize` package. The ADR-058 §3
  "duplicated helpers are deleted" invariant is now fully
  satisfied across the Go control plane — no
  `normalizeLabel` helper exists outside
  `observability/normalize` and `replication/metrics` (the
  latter is intentional, recorded in ADR-060 §1 and now §1
  here).
- The `OutcomeSuccess` / `OutcomeRejected` / `OutcomeFailure`
  constants and the `outcomeAllowlist` slice become the
  single source of truth for the connection-test outcome
  contract. Future contributors can copy the same pattern
  from the connection-test Recorder to any new Recorder
  owner.
- The connection-test long-running consumer can now host a
  Recorder through its own injected `prometheus.Registerer`
  (mirroring Phase 17 / 18 long-running consumer design) and
  expose the families from its own `/metrics` endpoint
  without competing for the global default registry.

### Negative

- Phase 19 is a strict refactor — no new families, no new
  label values, no new emission call sites. The value of the
  refactor is structural (one fewer duplicated helper) rather
  than functional. This is the same trade-off as slice 44.3
  recorded in ADR-060 §5; documenting it again so reviewers do
  not have to chase the cross-reference.
- The `outcomeAllowlist` slice is private to the
  `connectiontestmetrics` package; if a future Recorder owner
  reuses the same allowlist, it would either import
  `connectiontestmetrics` (a layering inversion) or copy the
  slice. The cleaner alternative is a future ADR that
  promotes the allowlist to `observability/normalize` as a
  named helper (`normalize.NormalizeOutcomeSuccess`, say).
  Recorded here as a Phase 20 candidate.
- The Scheduler module's `go.mod` already has the
  `observability` require + replace (Phase 18 slice 44.0); no
  new transitive dependency is added. There is no Maven or Go
  version-bump work for Phase 19.

## Alternatives Considered

### Skip Phase 19 and ship a one-off slice for connection-test migrate

Reject. The connection-test Recorder migrate is the natural
follow-up of Phase 18 slice 44.1; treating it as a one-off
would leave the ADR-058 §3 invariant partially satisfied.
A one-off slice also lacks an umbrella ADR, which is the
established Phase 17 / Phase 18 / Phase 19 discipline.

### Phase 19 = Java data-plane emission follow-up (26.F9)

Reject. 26.F9 touches the worker protocol and Java trust
binding, which are large architectural decisions. ADR-051 §7
records it as a future phase but does not constrain it to
Phase 19. This ADR explicitly excludes it.

### Phase 19 = replication Recorder migrate

Reject. The replication Recorder labels are not canonical
lowercase UUIDs and not bounded worker ids. Routing them
through `observability/normalize` would be a misuse. A
separate label helper (`FreeText` with a length cap) is the
correct design but is not Phase 19 scope. A future Phase 20
ADR can decide whether to introduce `FreeText` and route the
replication Recorder through it.

### Phase 19 = emission sub-slices (43.1.5 / 43.2.5 / 43.3.5)

Reject. Those sub-slices have open ownership questions (API
Server sign-in handler needs new RPC + RBAC role — AGENTS.md
§8 decision gate; controller reconcile path needs durable
commit decision; admin CLI Recorder needs the long-running
consumer question resolved). They are not a single coherent
Phase.

### Phase 19 = connection-test Recorder migrate (chosen)

Accept. The connection-test Recorder migrate is a strict
refactor that closes the last duplicated `normalizeLabel`
helper in the control plane. It does not require new
architectural decisions, and it follows the Phase 18 slice
template (45.0 umbrella + 45.1 Recorder migrate + 45.2 test
contract). The slice is small but structural.
