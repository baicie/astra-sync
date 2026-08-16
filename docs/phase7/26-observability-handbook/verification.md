# Slice 26 Verification

## Test plan

The slice is verified by the following checks.

### Template integrity

- `make check-runbooks` walks every `*.md` file under
  `docs/observability/` and asserts that:
  - Each file contains at least one `<placeholder>` token.
  - Each file does not contain a known production hostname pattern.
- The check fails the gate if any assertion fails.

The guard is the same one that Slice 24 introduced for the
operational runbooks. The `docs/observability/` directory inherits
the Slice 24 boundary because the handbook shares the same
template-vs-populated boundary.

### Handbook consistency

The handbook is verified by hand against the existing production
code:

- The metrics catalog must reference every metric name that the
  control plane and the data plane emit. The follow-up slice that
  registers the Prometheus client emits the metrics documented in
  the catalog.
- The log conventions must reference every logger name that the
  control plane and the data plane emit. The follow-up slice that
  migrates the Java data plane to SLF4J gives the Java executables
  the logger names documented in the conventions.
- The audit correlation must reference every audit column that the
  Slice 18 schema declares. The correlation joins the audit table
  to the metrics and the logs by the `request_id` field.
- The SLO handbook must inherit the per-slice SLIs from the Phase
  6 acceptance document. The cross-reference is the §"Per-slice
  SLIs" table.
- The dashboard recipes must consume the metrics documented in the
  catalog. The recipes reference the metric names by code block.

### Local checks

- `make check-runbooks` passes locally.
- The CI workflow runs the same guard on every PR that touches
  `docs/observability/`.

## Evidence

The slice is a documentation delivery. The verification evidence is
the local `make check-runbooks` run plus the CI run on the PR.

| Check | Local | CI |
|---|---|---|
| `make check-runbooks` | passes | passes (the `runbooks` job) |

## Acceptance

The slice is accepted when:

1. `make check-runbooks` passes locally and on the PR's CI run.
2. The Phase 7 README marks Slice 26 as Implementation Complete.
3. The metrics catalog, the log conventions, the audit correlation,
   the SLO handbook, and the dashboard recipes are consistent with
   the production code that ships today.

## Out of scope

The slice does not verify:

- The follow-up slice that migrates the Java data plane to SLF4J.
  The migration is a follow-up slice; the handbook documents the
  convention.
- The follow-up slice that registers the Prometheus client in the
  API Server, the Console, and the auth module. The registration is
  a follow-up slice; the handbook documents the convention.
- The populated dashboard. The dashboard is a deployment-owned
  artefact; the handbook provides reference queries that the
  dashboard consumes.

## References

- [ADR-047: Observability Handbook and Dashboard Consolidation](../../adr/adr-047-observability-handbook-and-dashboard-consolidation.md)
- [ADR-044: Phase 6 Closeout and Phase 7 Entry Criteria](../../adr/adr-044-phase6-closeout-and-phase7-entry-criteria.md)
- [ADR-020: CLI Metrics Report Contract](../../adr/adr-020-cli-metrics-report.md)
- [ADR-042: Tenant-scoped Audited Security Event Queries](../../adr/adr-042-tenant-scoped-audited-security-event-queries.md)
- [Phase 6 acceptance document](../../phase6/acceptance.md)