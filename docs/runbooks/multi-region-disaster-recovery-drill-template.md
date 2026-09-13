# Multi-Region Disaster Recovery Drill Runbook

**Template version**: 1.0.0
**Last updated**: <update-date>
**Owner**: <operator-team>
**Environment**: <staging|production-like>

> **WARNING**: This is a template. Do NOT commit populated values to source
> control. Replace all `<placeholder>` values with deployment-specific values
> in the deployment-side runbook store.

## Purpose

Use this drill to record a bounded primary-region outage, secondary
continuity, recovery prerequisites, and restoration evidence. The operator
must record whether the deployment has the prerequisites for a real
promotion and checkpoint recovery before treating those steps as successful.

## Preconditions

- [ ] Drill owner and approver are assigned: <approver>
- [ ] Job identifier: <job-id>
- [ ] Primary region: <primary-region>
- [ ] Secondary region: <secondary-region>
- [ ] Replication lag threshold: <replication-lag-threshold>
- [ ] Latest acknowledged sequence: <checkpoint-sequence>
- [ ] Promotion idempotency key: <promotion-idempotency-key>
- [ ] Recovery idempotency key: <recovery-idempotency-key>
- [ ] Secondary sink capability has been revalidated: <capability-status>
- [ ] Checkpoint manifest and state files are readable from the recovery
      object store: <checkpoint-readiness>
- [ ] Rollback and escalation contacts are available: <on-call-contact>

If any prerequisite is unknown, record it as `not verified` and execute the
fail-closed checks. Do not mark promotion or recovery complete from an
availability-only result.

## Evidence Capture

Record the following before the fault:

| Item | Value |
|---|---|
| Drill start | <drill-start-time> |
| Primary readiness result | <primary-ready-result> |
| Secondary readiness result | <secondary-ready-result> |
| Topology version or snapshot | <topology-version> |
| Checkpoint sequence and epoch | <checkpoint-sequence-and-epoch> |
| Replication status response | <replication-status-result> |
| Metrics query or dashboard | <metrics-reference> |

## Procedure

### 1. Confirm steady state

```text
<command to check primary readiness>
<command to check secondary readiness>
<command to read replication status>
<command to verify checkpoint visibility>
```

Save output at `<evidence-location>`.

### 2. Inject the primary outage

```text
<command to stop or isolate the primary API Server>
```

Record the fault time as `<fault-time>`. Wait for the deployment health
window `<outage-detection-window>` and record both HTTP and gRPC results.

Expected result:

- Primary HTTP readiness: unavailable.
- Primary gRPC endpoint: unavailable.
- Secondary HTTP readiness: available.
- Secondary gRPC endpoint: available.

### 3. Verify secondary continuity

```text
<command to read secondary topology>
<command to read secondary replication status>
<command to inspect acknowledged checkpoint sequence>
```

Record the observed sequence as `<secondary-observed-sequence>`. It must be
no lower than the pre-fault acknowledged sequence unless the drill records a
known replication lag and escalates it.

### 4. Verify promotion and recovery prerequisites

```text
<command to request operator promotion>
<command to request checkpoint-coupled recovery>
```

Record promotion result `<promotion-result>` and recovery result
`<recovery-result>`. When the deployment lacks dynamic topology, a sink
capability result, or a replicated checkpoint manifest, the expected outcome
is a stable fail-closed status such as `FailedPrecondition` or `NotFound`.
Record the missing prerequisite as `<missing-prerequisite>` and escalate
according to local policy.

### 5. Restore the primary region

```text
<command to start or heal the primary API Server>
<command to wait for primary HTTP readiness>
<command to wait for primary gRPC readiness>
```

Record restoration time `<restoration-time>` and the resulting readiness
checks. Keep the job stopped or operator-controlled until the deployment
owner confirms the active epoch and fencing state.

## Verification

- [ ] Primary outage was detected within `<outage-detection-window>`.
- [ ] Secondary remained reachable throughout `<secondary-observation-window>`.
- [ ] Secondary topology showed the expected configured roles.
- [ ] Replication status and acknowledged sequence were recorded.
- [ ] Promotion outcome and status code were recorded.
- [ ] Recovery outcome and status code were recorded.
- [ ] No promotion or recovery was marked complete without its prerequisites.
- [ ] Primary HTTP and gRPC readiness recovered.
- [ ] Epoch, fencing, audit, and metrics evidence was reviewed by
      `<reviewer>`.

## Rollback and Escalation

Stop the drill and escalate if:

- the secondary accepts writes before promotion is authorized;
- a stale epoch is accepted;
- promotion succeeds without topology and capability prerequisites;
- recovery succeeds without a verifiable checkpoint manifest and state;
- the primary cannot be restored within `<restore-timeout>`.

Escalation contacts:

1. On-call engineer: <on-call-contact>
2. Service owner: <service-owner-contact>
3. Incident commander: <incident-commander-contact>

## References

- [Phase 11 disaster recovery drill design](../phase11/28-multi-region-disaster-recovery/design.md)
- [Phase 11 verification](../phase11/28-multi-region-disaster-recovery/verification.md)
- [Multi-region failover runbook](multi-region-failover-template.md)
- [ADR-048: Multi-Region Control-Plane Replication Model](../adr/adr-048-multi-region-control-plane-replication.md)
- [ADR-049: Region-pinned Data-Plane Failover with Epoch Fencing](../adr/adr-049-region-pinned-data-plane-failover.md)

## Change Log

| Date | Version | Author | Changes |
|------|---------|--------|---------|
| <date> | 1.0.0 | <author> | Initial template |
