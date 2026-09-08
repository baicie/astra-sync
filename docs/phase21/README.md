# Phase 21: FreeText Helper + Replication Recorder Migrate (Slice 46)

## Status

**Complete.** Phase 21 closes the last remaining `normalizeLabel`
helper in the control plane by introducing a new `NormalizeFreeText`
helper in `io.astrasync/control-plane/observability/normalize` and
migrating the replication Recorder to route all label values through
`observability/normalize`. The ADR-058 §3 invariant — "duplicated
helpers are deleted as each slice lands" — is now fully satisfied
for every Recorder owner in the Go control plane. The slice 46.0 +
46.1 + 46.2 land in this Phase.

## Goals

1. Add `NormalizeFreeText(value, maxBytes, unknown)` to
   `observability/normalize` as the canonical helper for
   free-form Prometheus label values (region names, hostnames, etc.).
2. Migrate the replication Recorder
   (`control-plane/replication/metrics`) from its internal
   `normalizeLabel` helper to: `NormalizeFreeText` for
   `target_region` / `peer_region`; `NormalizeOutcome` for
   `event_type` with the documented `checkpoint | topology | health`
   allowlist; `NormalizeOutcome` for `outcome` with the documented
   `success | failure` allowlist.
3. Delete the internal `normalizeLabel` helper from
   `replication/metrics`.
4. Ship a test contract for `NormalizeFreeText` and for each
   `Observe*` method in the replication Recorder.

## Non-goals

- Re-touching api-server, auth, controller, scheduler, or
  connection-test metric packages — they are already wired through
  `normalize`.
- Touching Java data-plane emission (ADR-051 §7 `26.F9`).
- Touching emission sub-slices (43.1.5 / 43.2.5 / 43.3.5).

## Roadmap

| Slice | Description | Status |
|-------|-------------|--------|
| 46.0 | Umbrella (ADR-063) + observability `go.mod` check | Done |
| 46.1 | `NormalizeFreeText` in `observability/normalize` + tests | Done |
| 46.2 | `replication/metrics` migrate + tests | Done |

## Acceptance Criteria

| Criterion | Status |
|-----------|--------|
| ADR-063 accepted and indexed | Done (2026-09-08) |
| `NormalizeFreeText(value, maxBytes, unknown)` added to `observability/normalize` | Done (slice 46.1) |
| `NormalizeFreeText` trims whitespace, rejects Unicode control characters, rejects values exceeding `maxBytes`, collapses empty/whitespace to `unknown` | Done (slice 46.1 tests) |
| `replication/metrics` imports `observability/normalize` | Done (slice 46.2) |
| `ObservePromotion` routes `targetRegion` through `NormalizeFreeText` and `outcome` through `NormalizeOutcome` with `promotionOutcomeAllowlist` | Done (slice 46.2) |
| `ObserveEvent` routes `peerRegion` through `NormalizeFreeText`, `eventType` through `NormalizeOutcome` with `eventTypeAllowlist`, `outcome` through `NormalizeOutcome` with `eventOutcomeAllowlist` | Done (slice 46.2) |
| `ObserveRecovery` routes `targetRegion` through `NormalizeFreeText` and `outcome` through `NormalizeOutcome` with `recoveryOutcomeAllowlist` | Done (slice 46.2) |
| Internal `normalizeLabel` removed from `replication/metrics` | Done (slice 46.2) |
| `metrics-catalog.md` row status updated | Done (slice 46.2) |
| `scripts/run-go-modules.py vet` and `test` exit 0 across all 8 control-plane modules | Done |
| `python scripts/check-changelog.py` exits 0 | Done |
| `python scripts/release-dry-run.py` exits 0 | Done |

## Records

- [Design](46-replication-recorder/README.md) (slice 0 entry point)
- [ADR-063](../adr/adr-063-phase21-freetext-replication-recorder-migrate.md) —
  umbrella decision
- [ADR-062](../adr/adr-062-v0.5.0-release-cut.md) — Phase 20 (v0.5.0)
- [ADR-060](../adr/adr-060-phase18-scheduler-metrics-normalize.md) —
  Phase 18 (where replication FreeText was first recorded as Phase 20+ candidate)
- [ADR-058](../adr/adr-058-observability-catalog-backlog.md) —
  Phase 17 umbrella; the replication Recorder is the last Recorder
  owner to close the ADR-058 §3 invariant
