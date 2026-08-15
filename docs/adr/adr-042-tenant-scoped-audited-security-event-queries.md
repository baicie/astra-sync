# ADR-042: Tenant-scoped Audited Security Event Queries

## Status

Accepted

## Context

AstraSync already stores append-only audit events from public mutations, authorization denials,
authentication, catalog activation, and runtime credential materialization. Exposing the table
directly would bypass tenant authorization, leak unreviewed JSON attributes, and make unbounded
queries part of the public contract.

## Decision

Provide a read-only `AuditService` that requires `audit.read`, always scopes repository reads to one
tenant, and exposes an allowlisted event projection. Queries use bounded time windows and exact
filters. Pagination uses a server snapshot plus `(occurred_at, event_id)` keyset cursors protected
by an expiring HMAC token that is fenced by tenant and authorization policy revision.

Successful reads append a tenant-scoped `audit.list` event before returning. The API fails closed
when this read audit cannot be written. Events without a tenant are not available through the
tenant service.

## Consequences

- Tenant operators can investigate state changes without database access.
- Stored JSON is not automatically promoted into the public API when new writers add attributes.
- Append-only inserts do not reorder an in-progress paginated snapshot.
- Global security investigations and bulk export require a separate, more privileged design.
