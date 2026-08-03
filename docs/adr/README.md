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
