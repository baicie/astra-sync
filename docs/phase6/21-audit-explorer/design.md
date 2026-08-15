# Phase 6 Slice 21 Design: Tenant Audit Explorer

## Problem

Job, Connection, Scheduler, authentication, and authorization paths already append events to
`astrasync_security_audit_events`. Operators with `audit.read` cannot retrieve those events through
the public API or Console, so the write path does not yet provide an operational investigation
loop.

## Decisions

### Tenant boundary

Every public query requires a canonical tenant ID and `audit.read`. The API authorizes before
reading and the repository always includes `tenant_id = requested tenant` in the SQL predicate.
Events whose tenant is null are not projected through this service. There is no caller-selectable
platform-wide mode.

### Query bounds

The first request accepts an optional inclusive lower time bound, an optional inclusive upper time
bound, exact event-type filters, exact outcome filters, and a page size. The default window is 24
hours, the maximum window is 90 days, each filter list contains at most 20 ordered unique values,
and pages contain at most 100 events.

Results are ordered by `(occurred_at DESC, event_id DESC)`. A first request captures a server-side
snapshot time. Continuation tokens carry the complete query, snapshot, authorization policy
revision, and last key. Tokens are HMAC authenticated, expire after 15 minutes, and cannot be used
for another tenant or after the caller's policy revision changes.

### Projection and redaction

The repository decodes stored attributes into a bounded domain event, but the public service emits
only an explicit allowlist of operational scalar attributes. Unknown keys, nested values, arrays,
oversized values, and non-finite numbers are omitted. The service never returns raw JSON, provider
locators, credentials, tokens, SQL, remote response text, or stack traces.

### Read auditing

After a successful repository query and before returning the response, the service synchronously
appends an `audit.list` event for the same tenant. The event contains only query dimensions such as
page size and filter counts. If that append fails, the request fails closed. The newly appended
event is outside the already captured page and can appear on a later refresh.

### Console boundary

The Console BFF derives the tenant from the authenticated server-side session, checks
`audit.read`, and forwards the opaque access token and request ID. Browser input cannot supply a
different protobuf tenant ID. The activity view offers bounded time and outcome filters, loads
older pages through opaque tokens, and renders all values as escaped text.

## Failure Semantics

- malformed filters, timestamps, or tokens: `INVALID_ARGUMENT`;
- missing authentication: `UNAUTHENTICATED`;
- missing tenant membership or `audit.read`: `PERMISSION_DENIED`;
- stale token policy revision: `PERMISSION_DENIED` through normal authorization, or token scope
  mismatch when invoked directly;
- repository or read-audit failure: `INTERNAL` with no storage detail;
- token expiry: `INVALID_ARGUMENT`.

## Rollout

The service is read-only and uses the existing API authentication mode, TLS boundary, audit table,
and token key. No new mutation gate is needed. Rollback removes the service and Console route while
retaining all events and the additive query index.
