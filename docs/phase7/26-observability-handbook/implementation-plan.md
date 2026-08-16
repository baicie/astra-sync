# Slice 26 Implementation Plan

## Work breakdown

The slice is delivered as four commits. Each commit is reviewable
in isolation and passes the local check.

### Commit 1 — ADR

- Add `docs/adr/adr-047-observability-handbook-and-dashboard-consolidation.md`.
- Update `docs/adr/README.md` to index ADR-047.

### Commit 2 — Observability handbook

- Add `docs/observability/README.md`.
- Add `docs/observability/metrics-catalog.md`.
- Add `docs/observability/log-conventions.md`.
- Add `docs/observability/audit-correlation.md`.
- Add `docs/observability/slo-handbook.md`.
- Add `docs/observability/dashboard-recipes.md`.

### Commit 3 — Slice records

- Add `docs/phase7/26-observability-handbook/README.md`.
- Add `docs/phase7/26-observability-handbook/design.md`.
- Add `docs/phase7/26-observability-handbook/implementation-plan.md`.
- Add `docs/phase7/26-observability-handbook/verification.md`.

### Commit 4 — Phase 7 README + index

- Update `docs/phase7/README.md` to mark Slice 26 as Implementation
  Complete and to point at the slice's records.

## Verification steps

After the last commit, the operator runs:

```sh
make check-runbooks
```

The gate walks every Markdown file under `docs/observability/` and
rejects any file that lacks a `<placeholder>` or contains a known
production hostname pattern. The gate is the same one that Slice 24
introduced for the operational runbooks.

The slice is ready to merge when `make check-runbooks` passes
locally and the CI run on the PR passes.

## Risks and mitigations

| Risk | Mitigation |
|---|---|
| The handbook documents a metric name that the production code does not emit. | The follow-up slice that registers the Prometheus client in the control plane Go modules and the Java module must emit the metrics documented in the catalog. The handbook references the metric names today so the follow-up is mechanical. |
| The follow-up migration breaks the existing production behaviour. | The follow-up slice is gated by the Phase 7 acceptance document. The slice is documented in the Phase 7 ADR-047 Consequences section so the boundary between the documentation-only Slice 26 and the code-migration follow-up stays clear. |
| The handbook drifts from the Helm chart. | The Helm chart exposes the `monitoring.prometheus.port` and `logging.pattern` knobs; the handbook records the convention that the operator uses to wire the platform's signal store. A change to the chart values requires a corresponding update to the handbook. |
| The check-runbooks guard rejects a populated document. | The guard is best-effort. A false positive is fixable by removing the production hostname pattern or by adding a placeholder. The guard is consistent with the Slice 24 guard. |

## Rollout

The slice does not require a deployment rollout. The handbook is
documentation. The slice ships when the PR merges.

## Open questions

None. The slice scope is documented in ADR-047 and the design.