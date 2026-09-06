# Phase 10 Slice 27.1: Multi-Region Observability Integration Design

## Context

The multi-region replication package already records promotion, event, and
recovery durations and outcomes. Component tests proved each recorder in
isolation, while the API Server endpoint test used a synthetic collector.
The missing evidence was a single business-path check showing that samples
from the replication service and recovery manager are present in the API
Server scrape registry.

## Decision

Use one isolated `replicationmetrics.Bundle` for the test process. Inject its
recorder into the API Server replication service and the recovery manager,
execute the three service operations, and scrape the same bundle through
`metricsServer`.

The test uses in-memory fakes for recovery storage, manifest parsing,
validation, and state restoration. This keeps the unit suite deterministic
and avoids a database or network dependency while still traversing the
service and domain manager boundaries that own the samples.

## Metric Contract

| Operation | Metric family | Labels asserted |
|-----------|---------------|-----------------|
| `PushCheckpoint` | `astrasync_multi_region_event_total` | `us-east-1`, `checkpoint`, `success` |
| `PromoteRegion` | `astrasync_multi_region_promotion_total` | `eu-west-1`, `success` |
| `RecoverForPromotion` | `astrasync_multi_region_recovery_total` | `eu-west-1`, `success` |

The existing recorder normalization remains authoritative for blank labels,
and the component tests continue to cover failure outcomes and non-negative
durations.

## Boundary and Lifecycle

The API Server owns the optional dedicated metrics listener. The test invokes
its handler with the shared registry and shuts the server down through a
context and cleanup hook. No new listener, registry, protocol contract, or
runtime storage is introduced.

## References

- [ADR-047: Observability Handbook and Dashboard Consolidation](../../adr/adr-047-observability-handbook-and-dashboard-consolidation.md)
- [Metrics catalog](../../observability/metrics-catalog.md)
- [Phase 9 closeout](../../phase9/closeout.md)
