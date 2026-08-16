# Phase 7 Slice 25: Implementation Plan

## Scope

This document is the implementation plan for the Slice 25 design
cluster. It records the decisions a follow-on implementation slice
must make before code lands and the dependencies the
implementation slice inherits from the design cluster.

The slice is design-only. The implementation slice is deferred to
Phase 8+ on the user's prior guidance ("设计语言超 Phase 6 增量范")
and on the operational evidence the implementation slice needs
before it can defend its choices.

## Open Decisions Before Code

The implementation slice must answer the questions in
[`design.md`](design.md) §"Open Questions" before code lands. The
questions are repeated here for traceability:

1. **Checkpoint replication transport.** PostgreSQL logical
   replication, object-storage-backed write-ahead log, or
   Kubernetes-native CSI snapshot. The choice must defend against
   ADR-007's three-storage model.
2. **Region topology discovery.** Kubernetes `MultiClusterService`,
   Consul-style service mesh, or deployment-configured endpoint
   list. The choice must defend against ADR-043's trusted-proxy
   boundary.
3. **Auto-promotion vs operator-initiated promotion.** ADR-010
   records operator-initiated promotion. The implementation slice
   must record whether auto-promotion is in scope and, if so,
   what RBAC role authorizes it.
4. **Cross-region audit query.** Out of scope for this design
   cluster (ADR-050). The implementation slice must record the
   boundary and any future-slicing intent.
5. **Failure-detection threshold tuning.** The default 30-second
   threshold must be validated against the deployment's network
   characteristics. The implementation slice must record the
   validation evidence in a verification document.
6. **Region-pinned connector descriptor metadata.** The connector
   descriptor catalogue (ADR-040) does not currently record
   whether a connector is region-pinned. The implementation slice
   must record whether to add a `regionAffinity` field.
7. **Sink commit revalidation timeout.** The default 60-second
   timeout must be tuned against the deployment's network
   characteristics. The implementation slice must defend the
   choice against the failure-detection threshold.

## Dependencies

The implementation slice depends on:

- **Phase 7 Slice 23 (control-plane mTLS).** ADR-045 records the
  mutual-TLS boundary that ADR-048's cross-region gRPC channel
  inherits. The implementation slice must verify that the channel
  is mTLS-protected.
- **Phase 7 Slice 24 (operational runbook templates).** The
  implementation slice must add a multi-region failover runbook
  template under `docs/runbooks/`. The runbook is a deployment-
  side artefact that an operator owns.
- **Phase 7 Slice 26 (observability handbook).** The
  implementation slice must add a multi-region SLO category to
  the handbook if and only if it adds a new metric. The design
  cluster does not add a new metric; the implementation slice
  must record whether the category is needed.
- **Phase 6 Slice 22 (transport hardening).** ADR-043 records the
  trusted-proxy boundary that ADR-048's region-topology discovery
  inherits. The implementation slice must verify that the
  discovery path is trusted-proxy-aware.

## Out-of-Scope Decisions

The following decisions are explicitly out of scope for the
implementation slice:

- **Active-active multi-region.** ADR-010 is not weakened. A Job
  has one active execution epoch at a time, in one region.
- **Cross-region identity replication.** ADR-036 is not weakened.
  A tenant's RBAC scope does not silently extend across regions.
- **Multi-region data-source or data-sink support.** The connector
  descriptor catalogue (ADR-040) stays unchanged in the design
  cluster. The implementation slice must record whether to add a
  `regionAffinity` field; the decision is a follow-up.
- **A new observability signal.** The design cluster does not add
  a new metric. The implementation slice must record whether a
  new SLO category is needed.

## Verification Path

The implementation slice must record its verification evidence in
a verification document under `docs/phase7/25-multi-region/`. The
verification document follows the Phase 6 Slice 22 template:

- **Functional verification.** The failover sequence in
  [`design.md`](design.md) §"Failover Sequence" is exercised end
  to end. The verification must cover the three success paths
  (epoch bump, capability revalidation, checkpoint-coupled
  recovery) and the three failure paths (stale-epoch-commit
  rejected, sink revalidation timeout, checkpoint not fully
  replicated).
- **Invariant verification.** The seven Phase 7 invariants in
  `docs/phase7/README.md` §"Boundary Notes" must hold across the
  failover. The verification must include a test that asserts each
  invariant in the failover context.
- **Boundary verification.** The trusted-proxy boundary (ADR-043),
  the mutual-TLS boundary (ADR-045), and the optimistic-version
  conflict resolution (ADR-029) must hold across the failover.

## Rollout Strategy

The implementation slice must record a rollout strategy in the
verification document. The rollout strategy is at minimum:

- **Region-pair staging.** The implementation slice must be
  exercised against a staging region pair before any production
  rollout. The staging region pair mirrors the production topology
  but uses synthetic data.
- **Phased production rollout.** The implementation slice must
  roll out to production in phases: read-only secondary region,
  read-write secondary region with manual promotion, then
  read-write secondary region with operator-initiated promotion.
- **Operator runbook.** The Slice 24 runbook template must be
  filled in with the multi-region failover procedure before the
  read-write secondary region is enabled in production.

## Non-goals Recap

The implementation slice inherits the design cluster's non-goals:

- No multi-region control-plane code path that touches a Go source
  file outside of the multi-region package.
- No multi-region data-plane code path that touches a Java source
  file outside of the multi-region package.
- No new protobuf field, no new Kubernetes CRD field, no new Helm
  chart value, until the design cluster's open questions are
  answered and the implementation slice lands its own ADR that
  amends this plan.
