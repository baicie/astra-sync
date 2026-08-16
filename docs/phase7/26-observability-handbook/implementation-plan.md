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
| The handbook documents a metric name that the production code does not emit. | The catalog labels each family as emitted, descriptor-only, or reserved. Descriptor registration is verified separately from future business call-site instrumentation. |
| The follow-up migration breaks the existing production behaviour. | F1/F2 preserve stable CLI and liveness output while routing error paths through SLF4J. F3 uses module-local loggers and F4/F5 keep metrics fail-closed when disabled. |
| The handbook drifts from the Helm chart. | The Helm chart exposes the `monitoring.prometheus.port` and `logging.pattern` knobs; the handbook records the convention that the operator uses to wire the platform's signal store. A change to the chart values requires a corresponding update to the handbook. |
| The check-runbooks guard rejects a populated document. | The guard is best-effort. A false positive is fixable by removing the production hostname pattern or by adding a placeholder. The guard is consistent with the Slice 24 guard. |

## Rollout

The closeout requires no automatic rollout. Helm renders the optional
metrics listeners and ServiceMonitor resources only when enabled; the
default-disabled listener path remains unchanged. The implementation ships
when the closeout PR merges.

## Open questions

Business metric observations, bounded exemplars, and Java data-plane metric
families are intentionally deferred. The boundary is documented in ADR-047
and the observability handbook.
