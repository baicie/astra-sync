# Phase 7 Slice 25: Multi-Region Design

## Overview

The slice records the design language for multi-region standby,
failover, and recovery semantics. It does not implement anything.
The design language lives in three ADRs:

- **ADR-048** — how the control-plane PostgreSQL state and the
  durable Job record replicate across regions.
- **ADR-049** — how the data plane fails over a region-pinned Job
  without violating ADR-006 epoch fencing, ADR-009 capability
  negotiation, or ADR-027 sink commit semantics.
- **ADR-050** — how the tenant identifier (ADR-036) and the audit
  trail (ADR-042) survive a regional failover without cross-region
  identity replication.

This document walks through the failover sequence that those three
ADRs collectively enable, and records the open questions that a
follow-on implementation slice must answer before code lands.

## Topology

```text
                  +-----------------------+
                  |  Operator (control)  |
                  |  -- Console / CLI    |
                  +-----------+-----------+
                              |
                  +-----------v-----------+
                  |  API Server (region)  |   <-- region-local ingress
                  |  + OIDC + RBAC        |
                  +-----------+-----------+
                              |
                  +-----------v-----------+
                  | Controller (region)   |   <-- Lease election
                  | Scheduler (region)    |   <-- Lease election
                  +-----------+-----------+
                              |
                  +-----------v-----------+
                  | PostgreSQL (region)   |   <-- ADR-007 storage separation
                  |  - Job lifecycle      |
                  |  - Connection catalog |
                  |  - Audit              |
                  +-----------+-----------+
                              |
              +---------------+---------------+
              |                               |
              v                               v
   +-----------------------+       +-----------------------+
   | Region A (primary)    |       | Region B (standby)    |
   |  Coordinator active   |       |  Coordinator passive  |
   |  Workers running      |       |  Warm workers (opt.)  |
   |  Sink writes          |       |  No sink writes       |
   +-----------+-----------+       +-----------+-----------+
               |                               |
               +------- checkpoint ------------+
                     replication (asynchronous,
                     RPO = checkpoint interval,
                     per ADR-010)
```

Two regions per Job. Each region runs the full Phase 6 control plane
(API Server, Controller, Scheduler, PostgreSQL). The data plane runs
in exactly one region at a time, per ADR-010's "single active
region" rule. A failover moves the active region; it does not
introduce a second active region.

## Failover Sequence

The sequence follows ADR-010's "Detection → Epoch Bump → Fencing →
Recovery → Validation → Resume" outline, with the data-plane
failover gated by ADR-049's epoch-fence checkpoint and ADR-027's
sink-commit revalidation.

```text
1. Detection
   - The secondary region's Controller fails to renew its leader
     lease (ADR-031) AND/OR the secondary region's API Server cannot
     reach the primary region's PostgreSQL replication source for
     > (failure-detection-threshold).
   - The secondary region enters "failover-pending" mode. No data
     plane Coordinator is started yet.

2. Operator decision (manual)
   - ADR-010 reserves operator judgement for the failover decision.
     The secondary region's Controller does not auto-promote without
     an explicit operator command.
   - The operator invokes `astra-sync region promote --region <B>`
     against the API Server in region B. The command is a
     desired-state mutation that requires the operator's RBAC role
     (ADR-036) and that writes a new audit event (ADR-042).

3. Epoch bump
   - The promotion command issues a new epoch via the secondary
     region's etcd cluster (per ADR-006's "Obtain new Epoch from
     etcd"). The new epoch is monotonically greater than every
     epoch previously seen in either region.
   - The new epoch is written to the durable Job record in the
     secondary region's PostgreSQL. ADR-029's optimistic version is
     bumped together with the epoch.

4. Fencing
   - The secondary region's Coordinator, when it next starts, uses
     the new epoch to fence the old region's Coordinator and
     Workers. ADR-006's "Sink Committer Validation: If
     request.Epoch < currentEpoch: REJECT" rule applies across
     regions.
   - The old region's stale writers are rejected at the sink.

5. Recovery
   - The secondary region reads the latest completed checkpoint from
     the replicated checkpoint store (ADR-048 §"Checkpoint
     replication"). The replication is asynchronous per ADR-010, so
     the recovered epoch may not equal the last successful commit
     epoch; the secondary region resumes from the highest epoch
     that is durably replicated.
   - The Coordinator restarts from the recovered checkpoint. The
     data plane's existing resumable full-load (ADR-024) and
     checkpoint-coupled CDC (ADR-028) code paths apply unchanged.

6. Validation
   - Before the first sink write in the secondary region, the data
     plane re-runs the capability negotiation (ADR-009) against the
     sink. The sink's transactional / idempotent capability must be
     re-validated, not assumed.
   - The Sink Committer accepts the new epoch only after the
     validation succeeds. A failed validation aborts the failover
     and surfaces an audit event of type `region-promotion-aborted`.

7. Resume
   - The Coordinator enters the resumed epoch. The audit trail
     records the new epoch, the new active region, and the recovered
     checkpoint ID.
   - The API Server and Console now direct all subsequent requests
     for the Job to the secondary region. The old region's API
     Server and Console continue to serve other Jobs that have not
     been promoted.
```

## Recovery Point Objective

Per ADR-010, the RPO equals the checkpoint interval. The three ADRs
in this slice do not weaken that bound. ADR-048 records that the
checkpoint replication latency is asynchronous; a failover that
happens before the latest checkpoint replicates recovers from the
last replicated checkpoint and re-executes the uncommitted batch.
The re-execution is bounded by ADR-027's "transactional or
idempotent" rule: the sink must accept the re-execution without
duplication.

## Recovery Time Objective

The RTO is bounded by:

- `failure-detection-threshold` (default 30 seconds, configurable via
  the Helm chart value `multiRegion.failureDetectionThreshold`).
- The time to issue the operator-initiated promotion command. The
  command is a single gRPC call; the operator-side RTO is human.
- The time to fence the old region. ADR-006's fencing is
  synchronous against etcd; the latency is bounded by the etcd
  round-trip (sub-second in a healthy cluster).
- The time to recover from the replicated checkpoint. The
  Coordinator restart is bounded by the checkpoint size and the
  state-backend load (Phase 5 Slice 16 evidence applies; the
  restart time is dominated by the state-backend read, not by the
  multi-region code path).

The slice does not commit to a numeric RTO; the deployment owns the
SLO. The ADR-050 cross-region audit query interface records the SLO
boundary, but the audit query interface is not implemented in this
slice.

## Open Questions

A follow-on implementation slice must answer the following
questions before code lands. The questions are recorded here so a
future slice does not silently re-litigate them.

1. **Checkpoint replication transport.** ADR-048 §"Checkpoint
   replication" leaves the transport open. The candidate
   transports are: PostgreSQL logical replication, an
   object-storage-backed write-ahead log, or a Kubernetes-native
   CSI snapshot. The implementation slice must compare the three
   options against ADR-007's three-storage model.

2. **Region topology discovery.** How does the secondary region
   discover the primary region's PostgreSQL endpoint? A Kubernetes
   `MultiClusterService`, a Consul-style service mesh, or a
   deployment-configured endpoint list are all candidates. The
   implementation slice must defend the choice against ADR-043's
   trusted-proxy boundary.

3. **Auto-promotion vs operator-initiated promotion.** ADR-010
   records that the failover is operator-initiated. The design
   preserves that boundary, but a future operator may want
   auto-promotion under a stricter policy. The implementation slice
   must record whether auto-promotion is in scope and, if so,
   what RBAC role authorizes it.

4. **Cross-region audit query.** ADR-050 records that the audit
   query interface stays region-local. A future slice that wants a
   cross-region audit query must define the read consistency
   contract (strong vs eventual) and the tenant authorization
   boundary for the cross-region query.

5. **Failure-detection threshold tuning.** The default 30-second
   threshold must be validated against the deployment's network
   characteristics. The implementation slice must record the
   validation evidence in a verification document, the same way
   Slice 22 recorded the transport hardening verification.

6. **Region-pinned connector descriptor metadata.** The connector
   descriptor catalogue (ADR-040) does not currently record whether
   a connector is region-pinned. A future slice may need to add a
   `regionAffinity` field to the descriptor. The decision is
   deferred to the implementation slice.

7. **Sink commit revalidation timeout.** ADR-049 §"Capability
   revalidation" records that the sink capability must be
   re-validated before the first sink write. The validation timeout
   must be bounded; the implementation slice must pick the timeout
   value and defend it against the failure-detection threshold.

## What the Design Does Not Record

- The PostgreSQL replication topology. ADR-048 records the
  semantics; the topology (streaming, logical, snapshot) is the
  implementation slice's decision.
- The Helm chart values for the multi-region feature. The chart
  values are added when the implementation slice lands.
- The CLI command syntax. The `astra-sync region promote` shape is
  illustrative; the implementation slice must record the actual
  command and its RBAC requirement.
- The dashboard recipes for the multi-region feature. The Phase 7
  Slice 26 handbook records the existing dashboards; a future
  slice that adds multi-region metrics must follow the handbook's
  registration procedure.

## Verification Path

The slice is design-only, so the verification path is a review by
the project's ADR reviewers. The review must verify:

- The three ADRs do not weaken any of the seven Phase 7 invariants.
- The failover sequence in this document is internally consistent
  with the three ADRs.
- The open questions are recorded with enough context that a future
  implementation slice can answer them without re-deriving the
  design.
- The slice does not silently add a new SLO category, a new
  observability signal, or a new auth surface.
