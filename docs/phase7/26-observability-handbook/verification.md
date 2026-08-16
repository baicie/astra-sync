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
- F6: Foundation status reconciliation and explicit deferred-work records.
- F7: API Server authentication counter/histogram and authorized audit-query
  histogram observations, trusted tenant labels, canonical UUID exemplars,
  and OpenMetrics negotiation.

The three F7 metric families now produce business samples. Other
control-plane counter/histogram call sites and Java data-plane metric families
remain outside this follow-up. The handbook and dashboard recipes label those
contracts as pending rather than claiming that registration has produced
samples.

## Test plan

### Template integrity

- `make check-runbooks` checks every Markdown file under `docs/runbooks/`
  and `docs/observability/` for a `<placeholder>` token and rejects known
  production hostname patterns.

### Code and configuration

- Go unit tests and vet run through the repository Makefile for every Go
  module.
- API Server focused tests run twice and assert authentication
  `success`/`rejected`/`failure` exactly once, trusted tenant selection,
  authorization-gated audit-query observations, and OpenMetrics exemplar
  encoding/omission.
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
5. Remaining business instrumentation and exemplar coverage is explicitly
   recorded as follow-up rather than silently treated as complete.
6. F7 marks only its three API Server families active and leaves every other
   descriptor or reserved family pending.

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
| Closeout PR CI | passed on [#48](https://github.com/baicie/astra-sync/pull/48), run `31950884699` |

### F7 evidence

| Check | Result |
|---|---|
| API Server focused `go test -count=2` | passed locally on 2026-08-16 |
| `make SHELL=D:/install/Git/usr/bin/bash.exe check-runbooks` | passed locally on 2026-08-16 |
| `git diff --check` | passed locally on 2026-08-16 |
| `make SHELL=D:/install/Git/usr/bin/bash.exe check` | passed locally on 2026-08-16 |
| `make SHELL=D:/install/Git/usr/bin/bash.exe test-go` | passed locally on 2026-08-16 |
| `make test-java` | passed locally on 2026-08-16 (32-module reactor) |
| `make SHELL=D:/install/Git/usr/bin/bash.exe check-security` | passed locally on 2026-08-16 |
| `mvn -B -ntp verify` | passed locally on 2026-08-16 (32-module reactor) |
| F7 PR CI | pending |

## References

- [ADR-047: Observability Handbook and Dashboard Consolidation](../../adr/adr-047-observability-handbook-and-dashboard-consolidation.md)
- [ADR-044: Phase 6 closeout and Phase 7 entry criteria](../../adr/adr-044-phase6-closeout-and-phase7-entry-criteria.md)
- [ADR-020: CLI Metrics Report Contract](../../adr/adr-020-cli-metrics-report.md)
- [ADR-042: Tenant-scoped Audited Security Event Queries](../../adr/adr-042-tenant-scoped-audited-security-event-queries.md)
- [Phase 6 acceptance document](../../phase6/acceptance.md)
