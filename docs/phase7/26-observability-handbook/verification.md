# Slice 26 Verification

## Verified scope

The closeout verifies two layers of the observability delivery:

- F1/F2: SLF4J + Logback + Logstash JSON configuration and SLF4J error
  events for Coordinator, Worker, and the execution heartbeat. Stable CLI
  summaries remain on `stdout`/`stderr`.
- F3: module-local `slog` JSON loggers with a `component` field for the
  migrated Go entry points. No independent cross-module observability module
  or local `replace` is required.
- F4: Prometheus descriptor registration and optional dedicated `/metrics`
  listeners for API Server, Scheduler, Connection Test Executor, and Console;
  the auth package contains descriptors but the one-shot admin CLI has no
  listener.
- F5: Helm metrics ports, environment wiring, Service ports, ServiceMonitor
  resources, and fail-closed rendering when monitoring is disabled.

Business counter/histogram call sites, `request_id` exemplars, and Java
data-plane metric families are intentionally outside this closeout. The
handbook and dashboard recipes label those contracts as pending rather than
claiming that registration has produced samples.

## Test plan

### Template integrity

- `make check-runbooks` checks every Markdown file under `docs/runbooks/`
  and `docs/observability/` for a `<placeholder>` token and rejects known
  production hostname patterns.

### Code and configuration

- Go unit tests and vet run through the repository Makefile for every Go
  module.
- Java tests run through the repository Maven targets, including the
  Coordinator Logback configuration and error-path tests.
- `make check-security` protects the existing credential and trusted-proxy
  boundaries.
- Helm lint/render checks cover metrics enabled and disabled modes. The CI
  guard additionally requires four `LOG_LEVEL` entries when the four
  long-running observability-enabled Go components are rendered, even when
  metrics are disabled.
- `git diff --check` rejects whitespace errors before the closeout commit.

## Acceptance

The closeout is accepted when:

1. The local Makefile checks and focused module tests pass.
2. Helm renders with metrics fail-closed and logging still configured.
3. The handbook classifies every documented metric family as emitted,
   descriptor-only, or reserved.
4. The closeout PR CI is green and the resulting squash commit is present on
   `main`.
5. The remaining business instrumentation and exemplar work is explicitly
   recorded as follow-up rather than silently treated as complete.

## Evidence

The final local and CI results are recorded in the closeout PR and in this
table after verification:

| Check | Result |
|---|---|
| `git diff --check` | passed locally on 2026-08-16 |
| `make SHELL=D:/install/Git/usr/bin/bash.exe check` | passed locally on 2026-08-16 |
| `make test-java` | passed locally on 2026-08-16 |
| `mvn -B -ntp verify` | passed locally on 2026-08-16 (32-module reactor) |
| `make SHELL=D:/install/Git/usr/bin/bash.exe test-go` | passed locally on 2026-08-16 |
| `make SHELL=D:/install/Git/usr/bin/bash.exe check-security` | passed locally on 2026-08-16 |
| Prometheus metric packages with `go test -count=2` | passed locally on 2026-08-16 |
| Helm lint/render guard | passed locally: 4 `LOG_LEVEL` entries and 3 ServiceMonitors |
| Affected Docker image builds | CI required; local Docker daemon was unavailable |
| Closeout PR CI | pending PR |

## References

- [ADR-047: Observability Handbook and Dashboard Consolidation](../../adr/adr-047-observability-handbook-and-dashboard-consolidation.md)
- [ADR-044: Phase 6 closeout and Phase 7 entry criteria](../../adr/adr-044-phase6-closeout-and-phase7-entry-criteria.md)
- [ADR-020: CLI Metrics Report Contract](../../adr/adr-020-cli-metrics-report.md)
- [ADR-042: Tenant-scoped Audited Security Event Queries](../../adr/adr-042-tenant-scoped-audited-security-event-queries.md)
- [Phase 6 acceptance document](../../phase6/acceptance.md)
