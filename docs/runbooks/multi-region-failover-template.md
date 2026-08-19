# Multi-Region Failover Runbook

**Template version**: 1.0.0
**Last updated**: <update-date>
**Owner**: <operator-team>
**Environments**: <production|staging>

> **WARNING**: This is a template. Do NOT commit populated values to source control.
> Replace all `<placeholder>` values with deployment-specific values before use.

## Prerequisites

Before initiating a failover, verify the following:

- [ ] Primary region is unreachable or degraded
- [ ] Secondary region is healthy and reachable
- [ ] Replication lag is below threshold (`<replication-lag-threshold>` seconds)
- [ ] Latest checkpoint is replicated to secondary region
- [ ] Operator has `role/operator` RBAC role
- [ ] Audit trail is accessible for post-mortem

## Pre-failover Checks

Run these commands before initiating failover:

```bash
# Check replication lag
# <kubectl command to check replication lag>

# Verify secondary region health
# <kubectl command to check secondary region health>

# Check latest checkpoint
# <kubectl command to check latest checkpoint>

# Verify operator RBAC role
# <kubectl command to verify RBAC role>
```

## Failover Procedure

### Step 1: Initiate Promotion

```bash
# Promote standby region to primary
# <astra-sync CLI command or API call>

# Expected output:
# {
#   "job_id": "<job-id>",
#   "previous_region": "<primary-region>",
#   "new_region": "<secondary-region>",
#   "new_epoch": <epoch-number>,
#   "status": "promotion_in_progress"
# }
```

### Step 2: Monitor Promotion Status

```bash
# Monitor promotion status
# <kubectl command to watch promotion status>

# Expected status transitions:
# 1. promotion_pending
# 2. epoch_bumped
# 3. epoch_written
# 4. capability_revalidating
# 5. capability_confirmed
# 6. failover_complete
```

### Step 3: Verify Sink Capability

```bash
# Verify sink capability revalidation succeeded
# <kubectl command to check sink capability>

# Expected output:
# {
#   "status": "capability_confirmed",
#   "capability": "<exactly-once|at-least-once>"
# }
```

### Step 4: Verify Checkpoint Recovery

```bash
# Verify checkpoint was restored
# <kubectl command to check checkpoint restoration>

# Expected output:
# {
#   "status": "recovery_complete",
#   "checkpoint_uri": "<checkpoint-uri>",
#   "epoch": <epoch-number>
# }
```

## Post-failover Verification

After failover completes, verify:

- [ ] Job is running in secondary region
- [ ] Data is being replicated to sink
- [ ] No replication lag between source and sink
- [ ] Audit events recorded in secondary region
- [ ] Metrics show job processing

```bash
# Check job status
# <kubectl command to check job status>

# Check replication lag
# <kubectl command to check replication lag>

# Check audit trail
# <kubectl command to query audit trail>
```

## Rollback Procedure

If failover fails or must be rolled back:

### Option 1: Retry Promotion

```bash
# If promotion failed at epoch_bumped or earlier:
# <astra-sync CLI command to retry promotion>

# Wait for replication to catch up
# <wait for replication lag to clear>
```

### Option 2: Manual Failback

> **WARNING**: Manual failback should only be used if retry promotion fails.

```bash
# 1. Stop job in secondary region
# <kubectl command to stop job>

# 2. Verify primary region is reachable
# <kubectl command to check primary region>

# 3. Start job in primary region
# <kubectl command to start job>

# 4. Verify job is running in primary region
# <kubectl command to verify job status>
```

## Escalation

### When to Escalate

Escalate immediately if:

- Failover does not complete within `<failover-timeout>` minutes
- Sink capability revalidation fails repeatedly
- Checkpoint cannot be downloaded
- Data loss is suspected

### How to Escalate

1. **On-call Engineer**: <on-call-contact>
2. **Engineering Lead**: <engineering-lead-contact>
3. **Site Reliability Engineer**: <sre-contact>

### Information to Provide

When escalating, include:

- Job ID and tenant ID
- Primary and secondary regions
- Time failover was initiated
- Last known promotion status
- Relevant log excerpts
- Steps already taken

## Metrics and Alerts

### Key Metrics

| Metric | Threshold | Action |
|--------|-----------|--------|
| Replication lag | > `<replication-lag-threshold>`s | Investigate |
| Promotion duration | > `<promotion-timeout>`s | Escalate |
| Checkpoint size | > `<max-checkpoint-size>` | Optimize |
| Failover frequency | > `<max-failovers-per-day>` | Post-mortem |

### Relevant Alerts

- `astrasync_region_unreachable`
- `astrasync_replication_lag_high`
- `astrasync_promotion_failed`
- `astrasync_capability_revalidation_failed`
- `astrasync_checkpoint_recovery_failed`

## References

- [ADR-048: Multi-Region Control-Plane Replication Model](../adr/adr-048-multi-region-control-plane-replication.md)
- [ADR-049: Region-pinned Data-Plane Failover with Epoch Fencing](../adr/adr-049-region-pinned-data-plane-failover.md)
- [Phase 8 Slice 25 documentation](../phase8/25-multi-region/)

## Change Log

| Date | Version | Author | Changes |
|------|---------|--------|---------|
| <date> | 1.0.0 | <author> | Initial template |
