# Phase 8 Slice 25.3: Implementation Plan

## Scope

This document is the implementation plan for Slice 25.3. It records
the decisions, dependencies, and verification path for the operator-initiated
region promotion command.

## Decisions Made

### 1. Promotion Endpoint

**Decision**: REST endpoint `POST /api/v1/jobs/{job_id}/regions/{region}/promote`

The endpoint follows the existing Job mutation pattern with idempotency key
and expected version.

### 2. Authorization

**Decision**: Require `role/operator` RBAC role

The promotion is a critical operation that requires explicit operator
authorization.

### 3. Promotion States

**Decision**: State machine with explicit transitions

The promotion follows a well-defined state machine to ensure
observability and debuggability.

## Dependencies

| Dependency | ADR | Status | Notes |
|------------|-----|--------|-------|
| Slice 25.1 | ADR-048 | Complete | WAL transport and topology loader |
| Slice 25.2 | ADR-048 | Complete | Cross-region gRPC channel |
| Durable Job record | ADR-029 | Complete | Optimistic version bumping |
| Epoch assignment | ADR-006 | Complete | etcd-based epoch |
| RBAC | ADR-036 | Complete | Operator role |

## Implementation Tasks

### Phase 1: API and CLI

- [ ] Add `PromoteRegion` and `GetPromotionStatus` RPCs to replication.proto
- [ ] Implement promotion handler in API Server
- [ ] Add idempotency key handling
- [ ] Add promotion status tracking

### Phase 2: Core Promotion Logic

- [ ] Implement epoch bump logic
- [ ] Implement Job record update with optimistic version
- [ ] Add promotion state machine
- [ ] Add promotion timeout handling

### Phase 3: Integration

- [ ] Wire promotion to sink capability revalidation (Slice 25.4)
- [ ] Wire promotion to WAL consumer
- [ ] Add audit events
- [ ] Add promotion metrics

## Out-of-Scope

- Sink capability revalidation (Slice 25.4)
- Auto-promotion
- Cross-region audit query

## Verification

### Functional Verification

1. **Promotion succeeds**: Command succeeds, new region is active
2. **Epoch bumps**: New epoch > previous epoch
3. **Optimistic version**: Job record updated with new version
4. **State transitions**: All states reached in order

### Authorization Verification

1. **Operator role required**: Command rejected without role
2. **Tenant cannot promote**: Tenant RBAC does not include promotion

### Idempotency Verification

1. **Duplicate command**: Returns existing promotion status
2. **Different idempotency key**: Creates new promotion

### Failure Verification

1. **Sink timeout**: Promotion fails, audit event recorded
2. **Epoch conflict**: Promotion fails, audit event recorded
3. **Network partition**: Promotion fails, can be retried

## Open Questions

None. All decisions are made in the design document.