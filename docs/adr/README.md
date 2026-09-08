# Architecture Decision Records

This directory contains the Architecture Decision Records (ADRs) for AstraSync.

## ADR Index

| Number | Title | Status | Date |
|--------|-------|--------|------|
| [ADR-001](adr-001-control-data-plane-separation.md) | Control Plane and Data Plane Separation | Accepted | 2026-08-02 |
| [ADR-002](adr-002-direct-pipeline-mode.md) | Direct Pipeline Mode as Default | Accepted | 2026-08-02 |
| [ADR-003](adr-003-source-enumerator-split-reader-model.md) | Source Enumerator/Split/Reader Model | Accepted | 2026-08-02 |
| [ADR-004](adr-004-sink-writer-committer-model.md) | Sink Writer/Committer Model | Accepted | 2026-08-02 |
| [ADR-005](adr-005-checkpoint-boundary.md) | Checkpoint as Consistency Boundary | Accepted | 2026-08-02 |
| [ADR-006](adr-006-epoch-fencing.md) | Epoch Fencing for Coordinator | Accepted | 2026-08-02 |
| [ADR-007](adr-007-storage-separation.md) | Storage Separation (PostgreSQL/etcd/Object Storage) | Accepted | 2026-08-02 |
| [ADR-008](adr-008-row-and-arrow-formats.md) | Dual Format Support (Row and Arrow) | Accepted | 2026-08-02 |
| [ADR-009](adr-009-capability-negotiation.md) | Exactly-Once via Capability Negotiation | Accepted | 2026-08-02 |
| [ADR-010](adr-010-single-active-region.md) | Single Active Region per Job | Accepted | 2026-08-02 |
| [ADR-011](adr-011-bounded-pull-single-node-runtime.md) | Bounded Pull-based Single-node Runtime | Accepted | 2026-08-02 |
| [ADR-012](adr-012-strict-versioned-job-spec.md) | Strict Versioned JobSpec Boundary | Accepted | 2026-08-02 |
| [ADR-013](adr-013-descriptor-first-connector-planning.md) | Descriptor-first Connector Planning | Accepted | 2026-08-02 |
| [ADR-014](adr-014-local-runner-and-cli-boundary.md) | Local Runner and CLI Boundary | Accepted | 2026-08-03 |
| [ADR-015](adr-015-strict-csv-and-create-new-output.md) | Strict CSV and Create-new Output | Accepted | 2026-08-03 |
| [ADR-016](adr-016-jdbc-connector-contract-and-type-mapping.md) | JDBC Connector Contract and Type Mapping | Accepted | 2026-08-03 |
| [ADR-017](adr-017-jdbc-transaction-boundaries.md) | JDBC Transaction Boundaries | Accepted | 2026-08-03 |
| [ADR-018](adr-018-cross-connector-scalar-encoding.md) | Cross-connector Scalar Encoding | Accepted | 2026-08-03 |
| [ADR-019](adr-019-cooperative-cancellation.md) | Cooperative Cancellation Boundary | Accepted | 2026-08-03 |
| [ADR-020](adr-020-cli-metrics-report.md) | CLI Metrics Report Contract | Accepted | 2026-08-03 |
| [ADR-021](adr-021-distributed-batch-runtime.md) | Distributed Batch Runtime Boundary | Accepted | 2026-08-03 |
| [ADR-022](adr-022-jdbc-range-splits.md) | Connector Split Enumeration and Numeric JDBC Ranges | Accepted | 2026-08-03 |
| [ADR-023](adr-023-worker-network-protocol.md) | Versioned Worker Protocol and Bounded Remote Admission | Accepted | 2026-08-03 |
| [ADR-024](adr-024-resumable-full-load.md) | Split-level Resumable Full-load Execution | Accepted | 2026-08-04 |
| [ADR-025](adr-025-distributed-jdbc-operational-slice.md) | Distributed JDBC Operational Slice | Accepted | 2026-08-04 |
| [ADR-026](adr-026-checkpoint-fencing-foundation.md) | Durable Checkpoint and Epoch Fencing Foundation | Accepted | 2026-08-04 |
| [ADR-027](adr-027-transactional-idempotent-sink-commit.md) | Transactional or Idempotent Sink Commit | Accepted | 2026-08-04 |
| [ADR-028](adr-028-native-cdc-and-checkpoint-coupled-offsets.md) | Native CDC and Checkpoint-coupled Offsets | Accepted | 2026-08-05 |
| [ADR-029](adr-029-durable-desired-state-job-lifecycle.md) | Durable Desired-state Job Lifecycle | Accepted | 2026-08-05 |
| [ADR-030](adr-030-lease-fenced-scheduler-dispatch.md) | Lease-fenced Scheduler Dispatch | Accepted | 2026-08-05 |
| [ADR-031](adr-031-controller-convergence-and-ha.md) | PostgreSQL Lifecycle Convergence and Execution Liveness | Accepted | 2026-08-05 |
| [ADR-032](adr-032-bounded-arrow-batch-foundation.md) | Bounded Arrow Batch Foundation | Accepted | 2026-08-06 |
| [ADR-033](adr-033-adaptive-batch-and-parallelism-control.md) | Adaptive Batch and Parallelism Control | Accepted | 2026-08-06 |
| [ADR-034](adr-034-spillable-exchange-and-checkpoint-persistence.md) | Spillable Exchange and Checkpoint Persistence Optimization | Accepted | 2026-08-07 |
| [ADR-035](adr-035-namespace-scoped-read-only-job-console.md) | Namespace-scoped Read-only Job Console | Accepted | 2026-08-08 |
| [ADR-036](adr-036-external-oidc-and-local-tenant-authorization.md) | External OIDC and Local Tenant Authorization | Accepted | 2026-08-08 |
| [ADR-037](adr-037-transactional-control-plane-audit-trail.md) | Transactional Control-plane Audit Trail | Accepted | 2026-08-08 |
| [ADR-038](adr-038-desired-state-job-mutation-workflows.md) | Desired-state Job Mutation Workflows | Accepted | 2026-08-08 |
| [ADR-039](adr-039-canonical-side-effect-free-jobspec-validation.md) | Canonical Side-effect-free JobSpec Validation | Accepted | 2026-08-08 |
| [ADR-040](adr-040-deployment-authoritative-connector-catalog.md) | Deployment-authoritative Connector Descriptor Catalog | Accepted | 2026-08-08 |
| [ADR-041](adr-041-external-secrets-epoch-credential-materialization.md) | External Secret References and Epoch-scoped Credential Materialization | Accepted | 2026-08-08 |
| [ADR-042](adr-042-tenant-scoped-audited-security-event-queries.md) | Tenant-scoped Audited Security Event Queries | Accepted | 2026-08-10 |
| [ADR-043](adr-043-transport-hardening-and-trusted-proxy-boundary.md) | Transport Hardening and Trusted-Proxy Boundary | Accepted | 2026-08-15 |
| [ADR-044](adr-044-phase6-closeout-and-phase7-entry-criteria.md) | Phase 6 Closeout and Phase 7 Entry Criteria | Accepted | 2026-08-15 |
| [ADR-045](adr-045-control-plane-mtls-and-network-boundary.md) | Control-Plane Mutual TLS and Network Boundary | Accepted | 2026-08-15 |
| [ADR-046](adr-046-operational-runbook-templates.md) | Operational Runbook Templates | Accepted | 2026-08-16 |
| [ADR-047](adr-047-observability-handbook-and-dashboard-consolidation.md) | Observability Handbook and Dashboard Consolidation | Accepted | 2026-08-16 |
| [ADR-048](adr-048-multi-region-control-plane-replication.md) | Multi-Region Control-Plane Replication Model | Accepted | 2026-08-16 |
| [ADR-049](adr-049-region-pinned-data-plane-failover.md) | Region-pinned Data-Plane Failover with Epoch Fencing | Accepted | 2026-08-16 |
| [ADR-050](adr-050-tenant-identifier-and-audit-cross-region.md) | Tenant Identifier and Audit Cross-Region Semantics | Accepted | 2026-08-16 |
| [ADR-051](adr-051-java-data-plane-metrics-activation.md) | Java Data-Plane Metrics Activation (Slice 26 Follow-up F8) | Accepted | 2026-08-22 |
| [ADR-052](adr-052-cross-region-checkpoint-push-rpc.md) | Cross-Region Checkpoint Push RPC | Accepted | 2026-08-24 |
| [ADR-053](adr-053-checkpoint-wal-and-promotion-recovery-orchestration.md) | Checkpoint WAL and Promotion Recovery Orchestration | Accepted | 2026-08-25 |
| [ADR-054](adr-054-argocd-gitops-integration.md) | ArgoCD GitOps Integration | Accepted | 2026-09-07 |
| [ADR-055](adr-055-connector-catalog-lifecycle-automation.md) | Connector Catalog Lifecycle Automation | Accepted | 2026-09-07 |
| [ADR-056](adr-056-ci-hygiene-and-release-tooling.md) | CI / Test Hygiene & Release Tooling | Accepted | 2026-09-07 |
| [ADR-057](adr-057-v0.3.0-release-cut.md) | v0.3.0 Release Cut (Phases 13-16) | Accepted | 2026-09-07 |
| [ADR-058](adr-058-observability-catalog-backlog.md) | Observability Catalog Backlog (Phase 17) | Accepted — Superseded by Phase 25 closeout | 2026-09-08 |
| [ADR-059](adr-059-v0.4.0-release-cut.md) | v0.4.0 Release Cut (Phase 17 Observability Activation) | Accepted | 2026-09-08 |
| [ADR-060](adr-060-phase18-scheduler-metrics-normalize.md) | Phase 18 Scheduler Metrics Normalize (Slice 44) | Accepted | 2026-09-08 |
| [ADR-061](adr-061-phase19-connection-test-recorder-migrate.md) | Phase 19 Connection-Test Recorder Migrate (Slice 45) | Accepted | 2026-09-08 |
| [ADR-062](adr-062-v0.5.0-release-cut.md) | v0.5.0 Release Cut (Phase 18 Scheduler + Phase 19 Connection-Test Observability) | Accepted | 2026-09-08 |
| [ADR-063](adr-063-phase21-freetext-replication-recorder-migrate.md) | Phase 21 FreeText Helper + Replication Recorder Migrate (Slice 46) | Accepted | 2026-09-08 |
| [ADR-064](adr-064-v0.6.0-release-cut.md) | v0.6.0 Release Cut (Phase 21 FreeText + Replication Recorder Migrate) | Accepted | 2026-09-08 |
| [ADR-065](adr-065-phase22-auth-session-revoke-emission.md) | Phase 22 Emission Sub-Slice 43.2.5 — auth_session_revoke_total via admin CLI | Accepted | 2026-09-08 |
| [ADR-066](adr-066-phase23-controller-reconcile-emission.md) | Phase 23 Controller Emission Sub-Slice 43.3.5 — controller_job_state_total via reconcile boundary | Accepted | 2026-09-08 |
| [ADR-067](adr-067-v0.7.0-release-cut.md) | v0.7.0 Release Cut (Phase 22 Auth Session Revoke + Phase 23 Controller Reconcile Emission) | Accepted | 2026-09-08 |
| [ADR-068](adr-068-phase24-apiserver-session-revoke-emission.md) | Phase 24 Emission Sub-Slice 43.1.5 — apiserver_session_revoke_total via API Server revoke RPC | Accepted | 2026-09-08 |
| [ADR-069](adr-069-phase25-controller-epoch-fence-emission.md) | Phase 25 Emission Sub-Slice 51.1 — controller_epoch_fence_total via reconcile boundary | Accepted | 2026-09-08 |
| [ADR-070](adr-070-v0.8.0-release-cut.md) | v0.8.0 Release Cut (Phase 24 API Server Session Revoke + Phase 25 Controller Epoch Fence + Phase 26 Closeout) | Accepted | 2026-09-08 |
| [ADR-071](adr-071-phase27-tenant-id-label-on-syncjob-cr.md) | Phase 27 slice 49.1.5 — `astrasync.io/tenant-id` Label on SyncJob CR | Accepted | 2026-09-08 |
| [ADR-072](adr-072-phase28-console-tenant-id-egress.md) | Phase 28 Slice 28-A — Console BFF Forwards `x-astra-tenant-id` on Job Mutations | Accepted | 2026-09-08 |
| [ADR-073](adr-073-phase28-slice28b-console-syncjob-cr-dual-write.md) | Phase 28 Slice 28-B — Console Owns `SyncJob` CR Dual-Write via controller-runtime | Accepted | 2026-09-08 |
| [ADR-074](adr-074-phase29-api-server-consumes-tenant-id.md) | Phase 29 — API Server Consumes `x-astra-tenant-id` (Server-side Interceptor + `job.Job.tenant_id` Column) | Accepted | 2026-09-08 |
| [ADR-071](adr-071-phase27-tenant-id-label-on-syncjob-cr.md) | Phase 27 — Tenant-Id Label on SyncJob CR (Controller Surface) | Accepted | 2026-09-08 |
| [ADR-072](adr-072-phase28-console-tenant-id-egress.md) | Phase 28-A — Console BFF Tenant-Id Egress | Accepted | 2026-09-08 |
| [ADR-073](adr-073-phase28-slice28b-console-syncjob-cr-dual-write.md) | Phase 28-B — Console SyncJob CR Dual-Write (Slice 28-B) | Accepted | 2026-09-08 |
| [ADR-074](adr-074-phase29-api-server-consumes-tenant-id.md) | Phase 29 — API Server Consumes Tenant-Id (Server-Side Consumption) | Accepted | 2026-09-08 |
| [ADR-075](adr-075-phase30-chain-tenant-id-regression.md) | Phase 30 — Tenant-Id Envelope End-to-End Regression Chain | Accepted | 2026-09-08 |
| [ADR-076](adr-076-phase31-cross-module-chain-test.md) | Phase 31 — Tenant-Id Envelope Cross-Module Chain Test (BFF ↔ API Server ↔ Mutation Repository) | Accepted — §2 superseded by ADR-080 | 2026-09-08 |
| [ADR-077](adr-077-phase27-28-29-audit-backfill.md) | Phase 27 / 28 / 29 Audit — Backfill of Untracked Implementation Files | Accepted (audit-only) | 2026-09-08 |
| [ADR-078](adr-078-phase27-29-audit-clarification.md) | Phase 27 / 29 Audit Clarification — Real Diffs in Working-Tree Files (ADR-077 §Context correction) | Accepted (audit-only) | 2026-09-08 |
| [ADR-079](adr-079-phase31-implementation-corrections.md) | Phase 31 Implementation Corrections — MutationRepository, Metadata Key, Migration Fallback | Accepted — superseded on public-surface by ADR-080 | 2026-09-08 |
| [ADR-080](adr-080-phase31-cross-module-public-surface.md) | Phase 31 Cross-Module Fixture — Public-Surface Constraint (Go internal/ rule) | Accepted | 2026-09-08 |
| [ADR-081](adr-081-phase32-label-translation-cr-writer.md) | Phase 32 — Tenant-Id Label Translation at the SyncJob CR Writer (Layer-1 test + `IsCanonicalTenantID` guard on `realDualWriter.create`) | Accepted | 2026-09-09 |
| [ADR-082](adr-082-phase33-update-mutation-tenant-id-guard.md) | Phase 33 — Update-Mutation Tenant-Id Guard at the SyncJob CR Writer (`IsCanonicalTenantID` guard on `realDualWriter.update`, symmetric with Phase 32) | Accepted | 2026-09-09 |

## Template

When adding a new ADR, use this template:

```markdown
# ADR-XXX: Title

## Status
Proposed | Accepted | Deprecated | Superseded

## Context
The issue that prompted this decision.

## Decision
The change that is being made and why.

## Consequences
What becomes easier or more difficult as a result of this change.
```

## Process

1. Create a new file named `adr-XXX-title.md`
2. Fill in the template sections
3. Update this index with the new ADR
4. Set status to "Proposed" initially
5. After review, update status to "Accepted"
