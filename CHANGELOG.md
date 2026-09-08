# Changelog

All notable changes to this project are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [v0.4.0] - 2026-09-08

This release covers Phase 17 (Observability Catalog Backlog Activation).
All twelve acceptance criteria in `docs/phase17/README.md` are **Done**.
The release cut is recorded in ADR-059; the umbrella design decision is
ADR-058. Phase 17 closes the **activation** half of the catalog backlog
(Recorder + label-allowlist reuse + test contract). The **emission**
half (transitioning rows from "Recorder wired" to "emitted") requires a
non-zero production sample and is owned by Phase 18+.

### Added

- `control-plane/observability/normalize` (slice 43.0):
  shared label-allowlist helpers for control-plane Prometheus business
  metrics. Three pure functions — `NormalizeTenant`,
  `NormalizeOutcome`, `NormalizeWorkerID` — enforce the
  canonical-lowercase-UUID tenant rule (ADR-047), bounded outcome
  allowlists (ADR-058 §3), and length-bounded worker-id allowlists.
  Slices 43.1 + 43.2 + 43.3 import this package; future metric owners
  can do the same.

- `scripts/run-go-modules.py` and root `Makefile`: `GO_MODULES` now
  includes `control-plane/observability`, so `make vet-go` and
  `make test-go` cover the new module alongside the existing eight
  control-plane modules.

- `control-plane/api-server/internal/metrics`: Phase 17 slice 43.1
  (ADR-058) wires `apiserver_sign_in_total`,
  `apiserver_session_revoke_total`, and
  `apiserver_trusted_proxy_hsts_total` through the new Recorder.
  Three new methods — `ObserveSignIn`, `ObserveSessionRevoke`,
  `ObserveTrustedProxyHSTS` — funnel every label value through
  `io.astrasync/control-plane/observability/normalize` (slice 43.0).
  The Recorder now owns the *Vec references for all six business
  metric families exposed by the API Server; package-level CounterVec
  access is reserved for tests and the registration boundary. Four
  new boundary-driven tests cover the catalog authentication outcome
  allowlist (`success|rejected|failure`), worker-id-style actor_id
  normalisation, pre-auth tenant funneling, and a cross-call
  cardinality bound that asserts six distinct caller inputs produce
  only four series.

- `control-plane/api-server/cmd/server/main.go`: the trusted-proxy
  HSTS observer now routes the pre-auth tenant value through
  `normalize.NormalizeTenant` so the label contract is owned by the
  observability package end-to-end (ADR-058 §3). Existing scrape-level
  tests in `main_test.go` continue to pass with no behaviour change.

- `control-plane/api-server/go.mod`: require + replace
  `io.astrasync/control-plane/observability` so the api-server module
  imports the slice-43.0 normalize helper.

- `control-plane/controller/internal/metrics`: Phase 17 slice 43.3
  (ADR-058) wires `controller_job_state_total` and
  `controller_epoch_fence_total` through the Recorder. Two new methods
  — `ObserveStateTransition(tenantID, namespace, fromState, toState)`
  and `ObserveEpochFence(tenantID, outcome)` — funnel every label
  value through `io.astrasync/control-plane/observability/normalize`
  (slice 43.0). The Recorder replaces its locally-implemented
  `normalizeTenant` / `normalizeOutcome` helpers with the shared
  package, satisfying the ADR-058 §3 "duplicated helpers are deleted
  as each slice lands" invariant. The Recorder now owns *Vec
  references for three families: the existing
  `controller_job_controller_reconcile_duration_seconds`, the new
  state-transition counter (4 labels: tenant_id / namespace /
  from_state / to_state), and the new epoch-fence counter with the
  `success|fenced|failure` allowlist exclusive to this metric
  (ADR-058 §3). Three new boundary-driven tests cover the Job state
  machine (ADR-029), the epoch-fence outcome allowlist, and a
  cross-call cardinality bound that asserts eight distinct caller
  inputs across both families produce only eleven bounded series.

- `control-plane/controller/go.mod`: require + replace
  `io.astrasync/control-plane/observability` so the controller module
  imports the slice-43.0 normalize helper alongside the existing
  Prometheus dependency.

- `control-plane/auth/internal/authmetrics`: Phase 17 slice 43.2
  (ADR-058) wires a new Recorder that owns dedicated
  `auth_sign_in_total` and `auth_session_revoke_total` CounterVecs
  registered against an injected prometheus.Registerer. Two new
  methods — `ObserveSignIn(tenantID, outcome, requestID)` and
  `ObserveSessionRevoke(tenantID, requestID)` — funnel every label
  value through `io.astrasync/control-plane/observability/normalize`
  (slice 43.0). The Recorder uses `success | rejected | failure` as
  the outcome allowlist for `auth_sign_in_total` (matching
  `docs/observability/metrics-catalog.md`). The Recorder and a new
  `HandlerFor(gatherer)` entry point give a long-running consumer
  (API Server, Console forwarder) an isolated surface to expose the
  families from its own /metrics endpoint without competing for the
  global default registry. The package-level `AuthSignInTotal` /
  `AuthSessionRevokeTotal` CounterVecs (registered against the
  default registry) remain untouched so any pre-43.2 import path
  (legacy `Handler()`) continues to work; the slice-43.2 design is
  additive, not destructive. Seven new boundary-driven tests cover
  the success / rejected / failure canonical paths, the
  `_platform` self-scope, non-canonical UUID collapse
  (uppercase, braced, raw e-mail), outcome allowlist collapse
  (non-allowlisted + empty), nil-receiver safety, nil-registerer
  rejection, duplicate-registration rejection, and a cross-call
  cardinality bound that asserts nine distinct caller inputs
  across both families produce only eight bounded series.

- `control-plane/auth/go.mod`: require + replace
  `io.astrasync/control-plane/observability` so the auth module
  joins api-server + controller as a consumer of the slice-43.0
  normalize helper.

- ADR-058 (`docs/adr/adr-058-observability-catalog-backlog.md`):
  Observability Catalog Backlog Phase 17 umbrella decision — records
  the recorder owner, call site, and label normalization contract for
  every descriptor-only / unregistered business metric in
  `docs/observability/metrics-catalog.md`.

- ADR-059 (`docs/adr/adr-059-v0.4.0-release-cut.md`): v0.4.0 release
  cut decision. Refines the literal Phase 17 acceptance criterion
  (`metrics-catalog.md` row "pending" → "emitted") into a precise
  two-stage lifecycle: phase 17 owns "pending" → "Recorder wired";
  Phase 18+ owns "Recorder wired" → "emitted". Records the version
  bump (`pom.xml` 0.3.0 → 0.4.0, `Chart.yaml` 0.3.0 → 0.4.0) and the
  `check-changelog.py` exemption extension to `<= 17`.

- `docs/phase17/README.md`: phase README with roadmap (slices
  43.0 / 43.1 / 43.2 / 43.3), acceptance criteria, backlog snapshot,
  and ADR cross-references. Status flips from **In Progress.** to
  **Complete.** per ADR-059 §1 (activation-vs-emission split).

- `docs/observability/metrics-catalog.md`: every "pending" row in the
  Implementation status table, the auth-detail table, the
  Controller-detail paragraph, and the Follow-up section now points
  at the Phase 17 slice / ADR-058 section that owns the row instead
  of deferring without an owner.

## [Unreleased]

### Added

- `control-plane/scheduler/internal/metrics`: Phase 18 slice 44.1
  (ADR-060) wires `scheduler_job_assignment_total`,
  `scheduler_lease_takeover_total`, and
  `scheduler_job_reconcile_duration_seconds` through a new
  Recorder that owns dedicated CounterVec / HistogramVec
  registered against an injected `prometheus.Registerer`. Three
  new methods — `ObserveAssignment`, `ObserveLeaseTakeover`,
  `ObserveReconcile` — funnel every label value that derives
  from caller input through
  `io.astrasync/control-plane/observability/normalize`
  (slice 43.0). The `worker_id` label routes through
  `NormalizeWorkerID`; the `tenant_id` label routes through
  `NormalizeTenant`; the `outcome` label routes through
  `NormalizeOutcome` with documented allowlists per family
  (`success | rejected | failure` for
  `scheduler_job_assignment_total`; `success` for
  `scheduler_lease_takeover_total`). The package-level
  `JobAssignmentTotal` / `LeaseTakeoverTotal` /
  `JobReconcileDuration` `promauto` Vecs remain registered
  against the default registry so any pre-slice-44 import
  path continues to scrape the same series; the Recorder is
  strictly additive. Nine new boundary-driven tests cover the
  success / rejected / failure canonical paths, the
  `_platform` self-scope, non-canonical UUID collapse
  (uppercase, braced, raw e-mail), outcome allowlist collapse
  for both allowlists, empty / whitespace worker-id collapse,
  negative-duration clamping for the histogram, nil-receiver
  safety, nil-registerer rejection, duplicate-registration
  rejection, and a cross-call cardinality bound that asserts
  eleven distinct caller inputs across the three families
  produce only eleven bounded series.

- `control-plane/scheduler/go.mod`: require + replace
  `io.astrasync/control-plane/observability` so the scheduler
  module joins api-server + auth + controller as a consumer
  of the slice-43.0 normalize helper. Slice 44.0 (ADR-060).

- ADR-060 (`docs/adr/adr-060-phase18-scheduler-metrics-normalize.md`):
  Phase 18 umbrella decision — closes the last remaining
  control-plane Recorder-owner gap (the Scheduler metric
  package). Records scope (Recorder + observe methods +
  tests), non-goals (replication metrics with non-tenant /
  non-worker-id labels; Java data-plane emission follow-up
  `26.F9`), and acceptance criteria mirroring the Phase 17
  template.

- `docs/phase18/README.md`: phase README with roadmap
  (slices 44.0 / 44.1 / 44.2; slice 44.3 optional),
  acceptance criteria, and ADR cross-references.

- `control-plane/scheduler/internal/connectiontestmetrics`:
  Phase 19 slice 45.1 (ADR-061) migrates the
  `connection_test_total` Recorder from its internal
  `strings.TrimSpace` / `switch outcome` collapse to a single
  call through
  `io.astrasync/control-plane/observability/normalize`. The
  Recorder routes `tenant_id` through `NormalizeTenant` and
  `outcome` through `NormalizeOutcome` with the documented
  `success | rejected | failure` allowlist and
  `OutcomeFailure` as the default value for non-allowlisted
  inputs. The Recorder is the fifth control-plane Recorder
  owner to enforce the ADR-058 §3 contract; with this slice
  the "duplicated normalize helpers are deleted as each slice
  lands" invariant is fully satisfied across the Go control
  plane. A new `outcomeAllowlist` package-private slice is
  the single source of truth for the connection-test outcome
  contract (Phase 20 candidate: promote to
  `observability/normalize` as a named helper). A new
  `HandlerFor(gatherer)` entry point mirrors the slice-44.1
  design pattern so a long-running Connection Test Executor
  consumer can host the Recorder-owned registry from its own
  /metrics endpoint. The package-level `ConnectionTestTotal`
  `promauto` Vec remains registered against the default
  registry so any pre-slice-45 import path continues to
  scrape the same series; the Recorder is strictly additive.

- ADR-061 (`docs/adr/adr-061-phase19-connection-test-recorder-migrate.md`):
  Phase 19 umbrella decision — closes the last remaining
  duplicated `normalizeLabel`-style helper in the control
  plane (the connection-test Recorder). Records scope (Recorder
  migrate + observe methods + tests), non-goals (replication
  metrics with non-tenant / non-worker-id labels; Java
  data-plane emission follow-up `26.F9`; emission sub-slices
  43.1.5 / 43.2.5 / 43.3.5), and acceptance criteria mirroring
  the Phase 17 / Phase 18 template.

- `docs/phase19/README.md`: phase README with roadmap (slices
  45.0 / 45.1 / 45.2), acceptance criteria, and ADR
  cross-references.

## [v0.3.0] - 2026-09-07

This release covers Phase 13 through Phase 16, completing Kubernetes production
hardening, ArgoCD GitOps integration, connector catalog lifecycle automation, and
CI/test hygiene tooling. All phases are marked **Complete** in their respective
README files.

### Added

#### Phase 16: CI / Test Hygiene & Release Tooling

- `scripts/test_ci_workflow.py`: regression tests for every CI step added in
  Phase 13 (production profile), Phase 14 (staging profile, ArgoCD schema),
  and Phase 15 (catalog-check with diff fallback); 11 tests now guard CI
  invariants
- `scripts/check-runbook-templates.py`: new `--all` mode with per-root
  `template` vs `doc` enforcement; Phase 14 ArgoCD README and Phase 15
  catalog authoring guide added to scan roots
- `scripts/check-changelog.py`: guard that every `**Complete.**` phase
  is referenced in `## [Unreleased]`; exits 1 with actionable errors on drift
- `scripts/release-dry-run.py`: dry-run release checklist verifying Maven
  version, git SHA, proto inventory, and CHANGELOG coverage without mutating
  any file
- `make check-docs`: new Makefile target running the full documentation
  hygiene gate (`check-runbooks --all` + `check-changelog`)
- `make release-dry-run`: new Makefile target running the release dry-run
  script; `make check` now includes `check-docs`
- CI `check-docs` job: runs `make check-docs` on `docs` scope changes
- Phase 15 completion record and Phase 16 README with roadmap and acceptance
  criteria

#### Phase 15: Connector Catalog Lifecycle Automation

- CLI `catalog-export`: replaced hardcoded `--compiler-build 0.1.0-SNAPSHOT`
  with `$(git rev-parse --short HEAD)` / `${{ github.sha }}` in Makefile
  and CI so the embedded build id always matches the current commit
- CLI `catalog-print` subcommand: decodes a `ConnectorInventory` `.pb` into
  deterministic line-oriented `key=value` output for diff-friendliness
- `scripts/diff-catalog.py`: categorises catalog drift into
  build-version-only (exit 0) vs semantic drift (exit 1), prints bullet
  list and unified diff; invoked by CI on failure so developers see
  diagnostics inline
- `scripts/catalog-info.py`: human-readable summary of any catalog without
  rebuilding the CLI jar
- `scripts/test_catalog_scripts.py`: 10 unit tests covering parse_lines,
  categorise_diff, and format_summary
- `docs/catalog-authoring.md`: end-to-end authoring guide covering when to
  regenerate, how to add a connector, how to interpret catalog-check
  failures, and environment variable overrides
- Makefile `catalog-export`, `catalog-info`, `catalog-diff` targets

#### Phase 14: GitOps & Progressive Delivery with ArgoCD

- `deployment/argocd/application.yaml`: ArgoCD Application CR for
  single-cluster Helm deployments with automated sync, prune, and self-heal
- `deployment/argocd/applicationset.yaml`: ArgoCD ApplicationSet with
  matrix generator (clusters + git directories) for multi-cluster,
  multi-environment deployments
- `deployment/argocd/namespace.yaml`: least-privilege RBAC (ServiceAccount,
  Role, RoleBinding) scoped to `astrasync-*` resources and
  `argoproj.io/applications{,ets}` verbs; no secrets access, no
  cluster-wide permissions
- `deployment/helm/astrasync/values-staging.yaml`: staging values profile
  (replicas=2, autoscaling off, NetworkPolicy off, DEBUG logging) with
  CI lint and render validation
- `deployment/argocd/environments/{dev,staging,production}/values-override.yaml`:
  environment-specific Helm value overrides for ArgoCD layered rendering
- `deployment/argocd/README.md`: operator onboarding guide covering install,
  sync, rollback, and CI integration
- CI: staging profile step (3 PDBs, 0 HPAs, 0 NetworkPolicies,
  environment=staging, DEBUG log level) + ArgoCD CR schema step
- ADR-054: ArgoCD GitOps integration design decision

#### Phase 13: Kubernetes Production Hardening

- `deployment/helm/astrasync/values-production.yaml`: production values
  profile (replicas=3, autoscaling on, NetworkPolicy on, mTLS on, 50Gi PVC)
- Helm template wiring: all production-only resources (HPA, PDB,
  NetworkPolicy, mTLS secret volume) conditionally rendered only when
  `values-production.yaml` is applied
- Helm assertions: API server `production` environment marker, scheduler
  `production` environment marker, Console `production` environment marker,
  Connection Test Executor `production` environment marker
- `deployment/helm/astrasync/values.yaml` safety defaults: HPA and PDB
  enabled by default; NetworkPolicy off by default (opt-in per environment)
- CI production profile step: lint + render + 5 assertions (environment
  marker, HPA, PDB, NetworkPolicy, mTLS, replicas=3)
- ADR-053: production hardening design decision

## [v0.2.0] - 2026-09-07

This release covers Phase 1 through Phase 12, completing the multi-region disaster
recovery platform. All phases are marked **Complete** in their respective README files.

### Added

#### Phase 12: CI/CD Pipeline Integration for Multi-Region Tests

- `multi-region-acceptance` CI job triggered by Go / multi-region test / Compose changes
- Immutable commit SHA pinning for all third-party GitHub Actions
- Compose log collection and upload on failure (`always()` cleanup)
- Change detection job (`needs.changes.outputs.multi_region`) to avoid unnecessary runs
- Script-level test coverage for the CI workflow (`test_ci_workflow.py`,
  `test_run_multi_region_acceptance.py`)

#### Phase 11: Cross-Region Disaster Recovery Drills

- Repeatable two-region outage drill (`disaster_recovery_test.go`) covering:
  - Primary endpoint unavailability
  - Secondary continuity and replication status visibility
  - Promotion self-rejection for secondary
  - Recovery prerequisite validation with stable gRPC error codes
  - Primary recovery and endpoint restoration
- Stable `status.Code` mapping for domain errors in `replication/service.go`
- Operator drill template with evidence and escalation fields
  (`multi-region-disaster-recovery-drill-template.md`)

#### Phase 10: Multi-Region Observability Integration

- API Server `/metrics` exposure for replication bundle metrics
  (`promotion_attempts_total`, `checkpoint_events_total`, `recovery_attempts_total`)
- Normalization of empty labels to `_unknown`
- Controller reconcile metrics (`controller_reconcile_duration_seconds`,
  `controller_reconcile_total`)
- Console request metrics (`console_http_requests_total`, `console_http_request_duration_seconds`)
- Connection test metrics (`connection_test_duration_seconds`, `connection_test_total`)
- Trusted-proxy HSTS samples in API Server
- Observability handbook update with live metric families
- SLO handbook with burn-rate alerting recipes

#### Phase 9: Multi-Region Integration Testing

- Multi-region Docker Compose e2e testing framework
  (`tests/integration/multi-region/`, `tests/integration/multi-region/framework/`)
- Failover integration tests covering primary stop, secondary visibility,
  and promotion
- Recovery integration tests covering checkpoint WAL and promotion recovery
  orchestration
- Performance benchmarks for multi-region topology
- Chaos tests (slice 26-5) with fault injection
- Phase 9 design, implementation plan, and verification records

#### Phase 8: Multi-Region Implementation

- Cross-region gRPC channel with mutual TLS (mTLS)
- Multi-region replication topology (`replication.TopologyService`,
  `replication.ReplicationService`)
- Operator-initiated region promotion (`PromoteRegion` RPC)
- Sink capability revalidation on failover
- Checkpoint-coupled recovery protocol (ADR-052, ADR-053)
- Multi-region failover runbook template
- Phase 8 closeout documentation

#### Phase 7: Multi-Region and Operational Maturity

- Control-plane mutual TLS (mTLS) with certificate rotation support
  (`control-plane/auth/transport/headers.go`, `auth/transport/headers_test.go`)
- Operational runbook templates (ADR-046)
- Observability handbook and dashboard consolidation (ADR-047)
- Console SLO metrics instrumentation
- Helm chart metrics wiring and ServiceMonitor configuration

#### Phase 6: Platform (Web Console, RBAC)

- Namespace-scoped read-only Job Console (`console/`)
- Tenant audit explorer with security event queries
- Connector catalog and connection management
- Job mutation workflows (create, update, delete, pause, resume)
- Transport hardening: production TLS enforcement, trusted-proxy boundary
  (ADR-043)
- Phase 6 closeout and Phase 7 entry criteria (ADR-044)

#### Phase 5: Performance Optimization

- Bounded Arrow RecordBatch foundation (ADR-032)
- Adaptive batch and parallelism control (ADR-033)
- Spillable exchange and checkpoint persistence (ADR-034)

#### Phase 4: Control Plane HA

- Lease-fenced scheduler dispatch (ADR-030)
- Durable control-plane job lifecycle (ADR-029)
- Controller convergence and HA (ADR-031)

#### Phase 3: CDC (MySQL, PostgreSQL)

- Native CDC runtime with Debezium-backed MySQL and PostgreSQL sources
  (ADR-028)
- JDBC CDC sink with idempotent commit markers
- Snapshot-to-CDC handoff with low-watermark and schema history

#### Phase 2: Checkpoint and Exactly-Once

- Checkpoint and epoch fencing foundation (ADR-026)
- Transactional or idempotent sink commit protocol (ADR-027)

#### Phase 1: Distributed Batch Sync

- Connector split enumeration (ADR-022, ADR-003)
- Versioned worker network protocol (ADR-023)
- Resumable full-load execution (ADR-024)
- Distributed JDBC operational slice (ADR-025)

### Changed

- Go toolchain upgraded from 1.22 to 1.26 across all control-plane modules
- Arrow version bumped from 18.0.0 to 19.0.0
- Jackson BOM bumped to 2.22.1
- SLF4J API bumped to 2.0.18
- Various Go module dependencies bumped via Dependabot (pgx, grpc, protobuf,
  client-go, prometheus client, etc.)

### Fixed

- API listeners now start before peer runtime to ensure availability during
  region startup
- Observability closeout documentation corrections
- Replication service error mapping for stable gRPC codes

### Security

- Transport hardening: `api-server` and `console` require production TLS
- Trusted-proxy boundary enforces X-Forwarded-* header trust model
- Control-plane mTLS between regions

## [v0.1.0-phase0] - 2026-08-02

Initial release covering Phase 0 MVP: Protocol and Single-node Kernel.

### Added

- Protocol Buffer definitions (`api/protobuf/`, `protocol/`)
- Connector SPI (`connector-api/`)
- Engine runtime (`engine/`)
- Formats: Arrow, Row, JSON, Parquet
- Transforms: SQL, Mask, Schema
- Connectors: JDBC, MySQL CDC, PostgreSQL CDC, Kafka, File, Iceberg, ClickHouse,
  Debezium
- Control-plane skeleton (API Server, Controller, Scheduler, Catalog, Auth)
- Docker, Helm, and Operator deployment configurations
- Architecture Decision Records (ADR-001 to ADR-042)
- Phase documentation (Phase 0 through Phase 6 partial)
