# ADR-063: Phase 21 FreeText Helper + Replication Recorder Migrate (Slice 46)

## Status

Accepted

## Context

ADR-060 §1 and ADR-061 §1 both recorded the `replication/metrics`
internal `normalizeLabel` helper as a Phase 20+ candidate. Phase 20
was the v0.5.0 release cut and did not include this scope, so it
passes to Phase 21.

The `replication/metrics` package
(`control-plane/replication/metrics`) uses an internal helper:

```go
// control-plane/replication/metrics/metrics.go
func normalizeLabel(value string) string {
    value = strings.TrimSpace(value)
    if value == "" {
        return "_unknown"
    }
    return value
}
```

This helper is used for four distinct label roles in the replication
metrics families:

| Role | Catalog description | Normalization need |
|------|--------------------|--------------------|
| `target_region` | Region name; unset → `_unknown` | Free-form, trim + length cap + empty→`_unknown` |
| `peer_region` | Region name | Free-form, trim + length cap + empty→`_unknown` |
| `event_type` | `checkpoint | topology | health` (catalog row) | Bounded allowlist → `NormalizeOutcome` |
| `outcome` | `success | failure` (catalog rows) | Bounded allowlist → `NormalizeOutcome` with `failure` fallback |

The current `normalizeLabel` does not distinguish these roles.
In particular:

- `event_type` should be **bounded** to the catalog allowlist
  `checkpoint | topology | health`. If a future caller passes a value
  like `HEARTBEAT` or `peer_disconnect`, the current helper would
  emit it as-is, creating an unbounded cardinality explosion.
  `NormalizeOutcome` with the allowlist and `_unknown` as fallback
  would correctly collapse it.
- `outcome` should be **bounded** to `success | failure`. The current
  helper accepts any non-empty string as-is, including `OK`,
  `RETRYABLE_ERROR`, etc. The catalog says `outcome is success or
  failure`, so `NormalizeOutcome` with `failure` as fallback is the
  correct contract.
- `target_region` / `peer_region` are **free-form region names** that
  do not fit the `NormalizeTenant` UUID check and do not fit the
  `NormalizeWorkerID` bounded-length pattern exactly. A new
  `NormalizeFreeText` helper is the correct abstraction: trim,
  reject control characters, length cap, empty→`_unknown`.

The `observability/normalize` package (Phase 17 slice 43.0,
ADR-058) was intentionally designed to grow additional helpers
as each phase lands (ADR-058 §3: "the package is intentionally
small so that each owner can contribute helpers without blocking
on others"). The Phase 21 addition of `NormalizeFreeText` is the
natural extension of that design.

## Decision

### 1. Phase 21 scope: FreeText helper + replication Recorder migrate

Phase 21 follows the established phase template:

- slice 46.0 (umbrella + observability `go.mod` check) — recorded
  in this ADR. The `control-plane/observability` module already
  requires the `control-plane/replication` module
  (`control-plane/observability/go.mod` has the replace directive
  from Phase 18 slice 44.0). The replication module does NOT
  require the observability module (dependency direction: replication
  → observability, not the reverse). No new `go.mod` changes
  are required for either module; the observability module gains a
  new exported function that replication can call.
- slice 46.1 (`NormalizeFreeText` in `observability/normalize`) —
  adds a `NormalizeFreeText(value string, maxBytes int,
  unknown string) string` function with documented contract.
- slice 46.2 (replication Recorder migrate) — updates the
  `replication/metrics` Recorder to use `NormalizeFreeText` for
  `target_region` / `peer_region` and `NormalizeOutcome` for
  `event_type` / `outcome`, removing the internal `normalizeLabel`
  helper. Updates `metrics-catalog.md` row status.

Out of scope for Phase 21:

- **Java data-plane emission follow-up (ADR-051 §7 `26.F9`)** —
  same as ADR-060 §1 / ADR-061 §1: worker protocol trust binding
  is a large architectural decision. Phase 21 does not touch Java
  code.
- **Emission sub-slices (43.1.5 / 43.2.5 / 43.3.5)** — same as
  ADR-061 §1: open ownership questions (API Server sign-in handler
  needs new RPC + RBAC role — AGENTS.md §8 decision gate;
  controller reconcile path needs durable commit decision). They
  are not Phase 21 scope.
- **Phase 22+** — this ADR does not commit to a Phase 22 theme.
  Candidates remain: emission sub-slices, Java data-plane
  emission, or a new observability backlog item.

### 2. Slice 46.1: NormalizeFreeText design

The new function:

```go
// NormalizeFreeText returns a bounded free-form label value:
//
//   - After trimming, if the value is empty or contains Unicode
//     isControl runes, returns unknown.
//   - If the trimmed value exceeds maxBytes, returns unknown.
//   - Otherwise returns the trimmed value.
//
// maxBytes must be positive; unknown must be non-empty. The function
// does NOT lowercase, does NOT truncate to maxBytes, and does NOT
// strip control characters. It returns unknown so that a runaway
// caller cannot poison the cardinality of a free-form label.
//
// The function is named FreeText rather than FreeLabel to avoid
// confusion with the Prometheus label concept; "FreeText" describes
// the input class (unstructured string, not a UUID or bounded enum).
func NormalizeFreeText(value string, maxBytes int, unknown string) string
```

**Design rationale for each behavior:**

- **Trim**: matches `NormalizeWorkerID` and the legacy
  `replication/metrics.normalizeLabel` behavior. Unstructured
  input often arrives with accidental leading/trailing whitespace.
- **Reject control characters**: free-form text should not contain
  newlines (`\n`, `\r`) or tabs (`\t`) in a Prometheus label value
  because OpenMetrics parsing is line-oriented. A caller that passes
  `"us-east-1\n"` should get `_unknown`, not a label with a literal
  newline. We use `unicode.IsControl` as the check (covers all
  Unicode control categories).
- **Length cap via reject, not truncate**: matches the
  `NormalizeWorkerID` design ("long values are dropped rather
  than truncated so that two distinct callers cannot collide on
  the same truncated label"). A region name like
  `"a...a"` (1000 `a` chars) should NOT become `"aaa..."` (128
  chars) and collide with a legitimate `"aaa..."` region. It
  should become `_unknown`. This is the same cardinality-bound
  reasoning as `NormalizeWorkerID`.
- **`unknown` parameter is caller-supplied**: this allows the
  replication Recorder to use `"_unknown"` (matching the catalog
  row and the existing `normalizeLabel` behavior) while a future
  Recorder owner can use a different sentinel if their catalog row
  requires it. The parameter is non-nil-checked (panic if empty)
  to prevent silent misconfiguration.

**Parameters:**

- `value`: the raw label value from the caller.
- `maxBytes`: the maximum byte length of the trimmed value. Must
  be positive. The replication Recorder uses 128 (same as
  `workerIDMaxLen`).
- `unknown`: the sentinel returned for empty, control-char, or
  over-length values. Must be non-empty. The replication Recorder
  passes `"_unknown"`.

**Out of scope for `NormalizeFreeText`:**

- Lowercasing — free-form region names are not normalized to
  lowercase because the catalog does not require it. A region
  named `"us-east-1"` and `"US-EAST-1"` are distinct inputs
  that should be preserved. If a future catalog requires lowercase,
  a `NormalizeFreeTextLower` variant can be added.
- Truncation — per the "length cap via reject" design above.
- Whitespace normalization beyond trim — `"  us-east-1  "` becomes
  `"us-east-1"` (trim), not `"us-east-1"` (collapse internal
  spaces). Internal spaces in region names are intentional
  (some providers use them).

### 3. Slice 46.2: replication Recorder migrate

The migration updates the three `Observe` methods:

**ObservePromotion(targetRegion, outcome, duration):**
- `targetRegion` → `normalize.NormalizeFreeText(targetRegion, 128, "_unknown")`
- `outcome` → `normalize.NormalizeOutcome(outcome, promotionOutcomeAllowlist, OutcomeFailure)`

**ObserveEvent(peerRegion, eventType, outcome, duration):**
- `peerRegion` → `normalize.NormalizeFreeText(peerRegion, 128, "_unknown")`
- `eventType` → `normalize.NormalizeOutcome(eventType, eventTypeAllowlist, "_unknown")`
- `outcome` → `normalize.NormalizeOutcome(outcome, eventOutcomeAllowlist, OutcomeFailure)`

**ObserveRecovery(targetRegion, outcome, duration):**
- `targetRegion` → `normalize.NormalizeFreeText(targetRegion, 128, "_unknown")`
- `outcome` → `normalize.NormalizeOutcome(outcome, recoveryOutcomeAllowlist, OutcomeFailure)`

**Allowlists (package-private slices, mirror Phase 19 pattern):**

```go
// promotionOutcomeAllowlist is the canonical outcome allowlist for
// astrasync_multi_region_promotion_total. Matches catalog row:
// "outcome is success or failure."
var promotionOutcomeAllowlist = []string{OutcomeSuccess, OutcomeFailure}

// eventOutcomeAllowlist is the canonical outcome allowlist for
// astrasync_multi_region_event_total. Matches catalog row:
// "outcome is success or failure."
var eventOutcomeAllowlist = []string{OutcomeSuccess, OutcomeFailure}

// recoveryOutcomeAllowlist is the canonical outcome allowlist for
// astrasync_multi_region_recovery_total. Matches catalog row:
// "outcome is success or failure."
var recoveryOutcomeAllowlist = []string{OutcomeSuccess, OutcomeFailure}

// eventTypeAllowlist is the canonical event type allowlist for
// astrasync_multi_region_event_total. Matches catalog row:
// "event_type is checkpoint, topology, or health."
var eventTypeAllowlist = []string{"checkpoint", "topology", "health"}
```

**Outcome constants (package-level, same pattern as Phase 19):**

```go
const (
    OutcomeSuccess = "success"
    OutcomeFailure = "failure"
)
```

**`event_type` allowlist fallback = `"_unknown"`:**
The catalog says `event_type` is `checkpoint | topology | health`.
If a caller passes a non-allowlisted value, the correct behavior
is `_unknown` (not `failure`, which is reserved for `outcome`).
The `eventType` field is not an outcome; it is a categorical tag.
The `_unknown` sentinel matches the `target_region` / `peer_region`
pattern in the same metric family.

**Internal `normalizeLabel` helper deleted:**
The `normalizeLabel` function is removed from
`control-plane/replication/metrics/metrics.go`. All three
`Observe*` methods now route through `observability/normalize`.
The ADR-058 §3 invariant — "duplicated helpers are deleted as each
slice lands" — is satisfied for the replication Recorder.

**Test contract (slice 46.2):**
Mirrors Phase 19 slice 45.2 template:
- Table-driven test for happy / rejected / failure paths for each
  `Observe*` method.
- Non-canonical region name collapse (e.g., `"ALICE@acme"` → `_unknown`).
- Control character rejection (e.g., `"us-east-1\n"` → `_unknown`).
- Over-length region name rejection (e.g., `"a"*129` → `_unknown`).
- Non-allowlisted `event_type` collapse to `_unknown`.
- Non-allowlisted `outcome` collapse to `failure`.
- Cross-call cardinality bound test.
- Nil-receiver safety test.
- Backward-compatibility test confirming the `Bundle` registry
  continues to expose the same families via `Handler()`.

### 4. Acceptance criteria

Phase 21 closes when slices 46.0 + 46.1 + 46.2 land and all
criteria are Done. The Phase ships in the next release cut
(ADR-064, v0.6.0 candidate).

| Criterion | Status |
|-----------|--------|
| ADR-063 accepted and indexed | Done |
| `NormalizeFreeText(value, maxBytes, unknown)` added to `observability/normalize` | slice 46.1 |
| `NormalizeFreeText` trims, rejects control chars, rejects over-length values, collapses empty to `unknown` | slice 46.1 tests |
| `replication/metrics` imports `observability/normalize` | slice 46.2 |
| `ObservePromotion` routes `targetRegion` through `NormalizeFreeText` and `outcome` through `NormalizeOutcome` with `promotionOutcomeAllowlist` | slice 46.2 |
| `ObserveEvent` routes `peerRegion` through `NormalizeFreeText`, `eventType` through `NormalizeOutcome` with `eventTypeAllowlist`, `outcome` through `NormalizeOutcome` with `eventOutcomeAllowlist` | slice 46.2 |
| `ObserveRecovery` routes `targetRegion` through `NormalizeFreeText` and `outcome` through `NormalizeOutcome` with `recoveryOutcomeAllowlist` | slice 46.2 |
| Internal `normalizeLabel` removed from `replication/metrics` | slice 46.2 |
| `metrics-catalog.md` row status updated | slice 46.2 |
| `scripts/run-go-modules.py vet` and `test` exit 0 across all 8 control-plane modules | Done |
| `python scripts/check-changelog.py` exits 0 | Done |
| `python scripts/release-dry-run.py` exits 0 | Done |

## Consequences

### Positive

- The `replication/metrics` Recorder now has the same label
  normalization discipline as every other Recorder owner in the
  control plane: all label values that derive from caller input
  route through `observability/normalize`.
- The `event_type` allowlist (`checkpoint | topology | health`)
  prevents unbounded cardinality from future `event_type` values.
- The `outcome` allowlist (`success | failure`) prevents unbounded
  cardinality from future `outcome` values.
- The `NormalizeFreeText` function is available for any future
  Recorder owner that needs free-form label normalization. It is
  intentionally parameterized (`maxBytes`, `unknown`) so that a
  future caller can choose different caps or sentinels without
  modifying the function signature.
- The ADR-058 §3 invariant is satisfied for the replication
  Recorder: no `normalizeLabel` helper exists outside
  `observability/normalize` in the Go control plane.

### Negative

- The `NormalizeFreeText` function panics if `maxBytes <= 0` or
  `unknown == ""`. This is a deliberate design choice (fail fast
  on misconfiguration) but means a caller that accidentally passes
  0 or a negative `maxBytes` will panic. The alternative (return
  `unknown` silently) would hide configuration errors. The panic
  is acceptable because `maxBytes` and `unknown` are compile-time
  constants in the replication Recorder, not runtime values.
- The `event_type` allowlist fallback is `_unknown` (not `failure`).
  This is correct for the catalog row ("event_type is checkpoint,
  topology, or health") but slightly inconsistent with the
  `outcome` allowlist fallback of `failure`. The inconsistency is
  intentional: `event_type` is a categorical tag, not an outcome.
  A future reader should not conflate the two.
- Phase 21 is a strict refactor — no new families, no new label
  values, no new emission call sites. The value is structural
  (one fewer duplicated helper, one additional normalize
  helper). This is the same trade-off as Phase 19.

## Alternatives Considered

### Use NormalizeWorkerID for region names

Reject. `NormalizeWorkerID` is designed for bounded-length worker
identifiers with a specific error sentinel (`_unknown`). Region
names are not worker identifiers and have different cardinality
characteristics. A separate `NormalizeFreeText` helper is the
correct abstraction.

### Lowercase region names in NormalizeFreeText

Reject. The catalog does not require lowercase region names. Some
cloud providers use mixed-case region names. Normalizing to
lowercase would break observability for those inputs. If a future
catalog requires lowercase, a `NormalizeFreeTextLower` variant
can be added without modifying `NormalizeFreeText`.

### Truncate over-length values instead of rejecting them

Reject. Truncation creates collision risk between distinct
long inputs that share the same truncated prefix. The
`NormalizeWorkerID` precedent is "drop rather than truncate so
that two distinct callers cannot collide on the same truncated
label." The same reasoning applies to region names.

### Leave replication/metrics as-is and address it in a later phase

Reject. The replication Recorder is the last remaining
`normalizeLabel` helper in the Go control plane. Leaving it
as-is means the ADR-058 §3 invariant is only partially satisfied.
A future contributor who adds a new Recorder owner might copy
the replication pattern instead of routing through
`observability/normalize`, re-introducing the duplication.

### Use a single NormalizeOutcome for all replication outcome labels

Reject. The three replication `*_total` families have the same
catalog description ("outcome is success or failure") but the
allowlist source of truth should be per-family for future
maintainability. If a future ADR adds a new outcome value to
`astrasync_multi_region_promotion_total` but not to the other
two families, the per-family slice makes that distinction
explicit. Using a single shared slice would hide that
distinction.
