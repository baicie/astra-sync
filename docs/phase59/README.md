# Phase 59 - Console Request ID Exemplars

## Status

**Complete.**

Phase 59 assigns one request ID at the Console BFF HTTP boundary and uses it
for Console metrics, downstream gRPC metadata, and auth-flow context.

References:
[ADR-106](../adr/adr-106-console-request-id-exemplars.md),
[ADR-047](../adr/adr-047-observability-handbook-and-dashboard-consolidation.md)

---

## Goal

```text
HTTP request -> one request_id
             -> console_request_total                 -> exemplar
             -> console_render_duration_seconds       -> exemplar
             -> downstream gRPC x-request-id metadata
             -> auth-flow request context
```

---

## Delivered Files

```text
console/observability/
├── metrics.go
└── metrics_test.go

console/internal/server/
├── observability.go
├── server.go
└── bff_test.go

docs/adr/
└── adr-106-console-request-id-exemplars.md

docs/phase59/
└── README.md
```

---

## Covered Families

```text
console_request_total
console_render_duration_seconds
```

The BFF reuses a non-empty `X-Request-ID` of at most 128 bytes and generates a
UUID when the header is missing or oversized. Only canonical lowercase UUIDs
are accepted as exemplars. The ID is never added to the normal label set.

The backend context builder reuses the request-boundary ID rather than
generating a second value. The Console `/metrics` handler enables OpenMetrics
negotiation, while requests without an OpenMetrics media range retain the
Prometheus text response.

---

## Verification

- Console metrics tests verify canonical exemplar coverage for the request
  counter and render histogram.
- Console metrics tests verify empty, non-UUID, uppercase, and oversized
  request IDs produce ordinary samples without exemplars.
- BFF tests verify an explicit request ID is shared by the observation and
  downstream gRPC metadata.
- BFF tests verify a missing header generates one canonical UUID and the
  observation and downstream request use that same value.
- The Console `/metrics` test verifies OpenMetrics content negotiation.

---

## Acceptance Criteria

- [x] One request ID is fixed at the Console BFF HTTP boundary.
- [x] Console metrics and downstream gRPC metadata share the request ID.
- [x] The auth flow receives the same request ID in its context.
- [x] Canonical request IDs appear as exemplars.
- [x] Invalid or missing request IDs produce ordinary samples.
- [x] `request_id` remains absent from normal metric labels.
- [x] ADR-106 is indexed and CHANGELOG includes the Phase 59 entry.

---

## Non-Goals

- No metric family or normal label change.
- No protocol field change.
- No tracing backend.
- No Helm, CRD, storage, or dependency change.

---

## Rollback

Remove the BFF request-boundary ID assignment and context propagation, restore
backend-local ID generation, remove the request-ID argument from the Console
Recorder, and restore the default Console `/metrics` content negotiation.
