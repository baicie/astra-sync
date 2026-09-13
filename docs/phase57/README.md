# Phase 57 - Auth Metric Request ID Exemplars

## Status

**Complete.**

Phase 57 attaches canonical `request_id` exemplars to the remaining
authentication and session-revocation counters.

References:
[ADR-104](../adr/adr-104-auth-metric-request-id-exemplars.md),
[ADR-047](../adr/adr-047-observability-handbook-and-dashboard-consolidation.md)

---

## Goal

```text
canonical request_id -> sign-in / session-revoke counter -> exemplar
invalid request_id   -> ordinary counter sample           -> no exemplar
API session revoke   -> audit row and metric share one request_id
```

---

## Delivered Files

```text
control-plane/api-server/internal/
├── metrics/metrics.go
├── metrics/metrics_test.go
├── service/access_service.go
└── service/access_service_test.go

control-plane/auth/internal/authmetrics/
├── metrics.go
└── metrics_test.go

docs/adr/
└── adr-104-auth-metric-request-id-exemplars.md

docs/phase57/
└── README.md
```

---

## Covered Families

```text
apiserver_sign_in_total
apiserver_session_revoke_total
auth_sign_in_total
auth_session_revoke_total
```

Only canonical lowercase UUIDs are accepted as exemplars. The ID is never
added to the normal label set.

The API Server session-revoke path derives one request ID before the audit
write and reuses it for every tenant observation, so the audit row and metric
exemplars describe the same operation.

---

## Verification

- API Server metrics tests verify canonical exemplar coverage across all
  covered counters and omission for non-canonical values.
- API Server service tests verify the session-revoke audit row and metric
  exemplars share the same request ID.
- Auth-library metrics tests verify canonical exemplar coverage and
  non-canonical omission for sign-in and session revoke.
- Existing bounded-label and nil-receiver tests remain green.

---

## Acceptance Criteria

- [x] All four auth-related counters attach canonical request-ID exemplars.
- [x] Invalid or missing request IDs produce ordinary samples.
- [x] `request_id` remains absent from normal metric labels.
- [x] API Server session-revoke audit and metrics share one request ID.
- [x] Existing metric names, label allowlists, and registration behavior are
  unchanged.
- [x] ADR-104 is indexed and CHANGELOG includes the Phase 57 entry.

---

## Non-Goals

- No new metric family or label.
- No protocol or identity-source change.
- No tracing backend.
- No Helm, CRD, storage, or deployment change.
- No change to auth outcome semantics.

---

## Rollback

Remove exemplar attachment from the four auth-related Recorder methods and
restore the API Server session-revoke metric call to two arguments.
