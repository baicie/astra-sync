# Phase 6 Slice 21: Tenant Audit Explorer

## Status

Implementation complete.

Slice 21 turns the existing append-only security audit trail into a usable tenant operations
surface. It adds a bounded, read-audited API and an authenticated Console activity view without
changing audit ownership or exposing arbitrary persisted JSON attributes.

## Intended Outcomes

- A tenant-scoped `AuditService` with bounded time windows, exact filters, and HMAC-protected
  keyset pagination.
- Direct API authorization through the existing `audit.read` permission and deny-by-default RPC
  registry.
- A safe event projection that exposes only reviewed operational attributes and never raw stored
  JSON.
- Synchronous audit records for successful audit reads so access to the trail is itself visible.
- A responsive Console activity workflow with stable pagination and tenant switching.

## Records

- [Design](design.md)
- [Implementation plan](implementation-plan.md)
- [Verification](verification.md)
- [ADR-042: Tenant-scoped Audited Security Event Queries](../../adr/adr-042-tenant-scoped-audited-security-event-queries.md)

## Boundary

This slice is read-only. It does not add tenant or membership administration, global audit search,
retention mutation, export, alerting, or arbitrary attribute queries. Platform-wide events with no
tenant ID remain outside tenant results.
