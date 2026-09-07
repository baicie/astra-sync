# Changelog

All notable changes to this project are documented in this file.
The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

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
