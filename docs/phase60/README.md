# Phase 60 - Connection Test Request ID Exemplars

## Status

**Complete.**

Phase 60 uses the durable Connection Test operation ID as the request ID for
the authoritative `connection_test_total` sample.

References:
[ADR-107](../adr/adr-107-connection-test-request-id-exemplars.md),
[ADR-047](../adr/adr-047-observability-handbook-and-dashboard-consolidation.md)

---

## Goal

```text
claimed Connection test -> durable CompleteTest
                        -> connection_test_total -> request_id exemplar
                        -> astrasync_connection_tests.operation_id
```

---

## Delivered Files

```text
control-plane/scheduler/internal/connectiontestmetrics/
├── metrics.go
└── metrics_test.go

control-plane/scheduler/internal/connectiontest/
├── executor.go
└── executor_test.go

docs/adr/
└── adr-107-connection-test-request-id-exemplars.md

docs/phase60/
└── README.md
```

---

## Covered Family

```text
connection_test_total
```

The exemplar value is the canonical UUID validated on the claimed
`TestOperation` and persisted as the primary key in
`astrasync_connection_tests`. The ID is never added to the normal label set.

The production Connection Test Executor `/metrics` handler enables OpenMetrics
negotiation. Requests without an OpenMetrics media range retain the
Prometheus text response.

---

## Verification

- Connection-test metrics tests verify a canonical operation ID appears as an
  exemplar.
- Connection-test metrics tests verify empty, non-UUID, uppercase, and
  oversized request IDs produce ordinary samples without exemplars.
- Executor tests verify the durable completion path passes the claimed
  `OperationID` to the metric sample.
- The production handler test verifies OpenMetrics content negotiation.
- Existing bounded-label, nil-receiver, registration, and lease-loss tests
  remain green.

---

## Acceptance Criteria

- [x] The authoritative Connection Test sample uses the durable operation ID.
- [x] Canonical operation IDs appear as `request_id` exemplars.
- [x] Invalid or missing IDs produce ordinary samples.
- [x] `request_id` remains absent from normal metric labels.
- [x] The production `/metrics` handler negotiates OpenMetrics.
- [x] Durable-completion and lease-loss semantics are unchanged.
- [x] ADR-107 is indexed and CHANGELOG includes the Phase 60 entry.

---

## Non-Goals

- No new protocol field or request identifier.
- No metric family or normal label change.
- No Controller lifecycle exemplar change.
- No Helm, CRD, storage, or dependency change.

---

## Rollback

Remove the operation-ID argument from the Connection Test Recorder, restore
the production `/metrics` handler's default content negotiation, and remove
the Phase 60 documentation entries.
