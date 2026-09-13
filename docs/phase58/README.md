# Phase 58 - Scheduler Request ID Exemplars

## Status

**Complete.**

Phase 58 connects Scheduler production metrics to the shared Recorder and uses
one request ID across every observation and failure log from a reconciliation
tick.

References:
[ADR-105](../adr/adr-105-scheduler-request-id-exemplars.md),
[ADR-047](../adr/adr-047-observability-handbook-and-dashboard-consolidation.md)

---

## Goal

```text
Scheduler Tick -> one request_id
               -> lease takeover metric -> exemplar
               -> assignment metric     -> exemplar
               -> reconcile duration    -> exemplar
               -> failure log           -> request_id
```

---

## Delivered Files

```text
control-plane/scheduler/internal/metrics/
├── metrics.go
└── metrics_test.go

control-plane/scheduler/internal/scheduler/
├── scheduler.go
└── scheduler_test.go

docs/adr/
└── adr-105-scheduler-request-id-exemplars.md

docs/phase58/
└── README.md
```

---

## Covered Families

```text
scheduler_job_assignment_total
scheduler_lease_takeover_total
scheduler_job_reconcile_duration_seconds
```

Only canonical lowercase UUIDs are accepted as exemplars. The ID is never
added to the normal label set.

The Reconciler defaults to the process-global Recorder and UUID generator, so
existing construction remains compatible. Tests and embedding callers can
inject isolated metrics and deterministic request IDs.

---

## Verification

- Scheduler metrics tests verify canonical exemplar coverage and
  non-canonical omission for all three families.
- Scheduler reconciler tests verify one Tick produces three exemplars with the
  same request ID through an injected Recorder.
- Scheduler logging tests verify reconciliation failures include `request_id`.
- Existing assignment, lease-takeover, reconcile-duration, normalization, and
  global-registry compatibility tests remain green.

---

## Acceptance Criteria

- [x] Scheduler production metrics use the shared Recorder.
- [x] One Tick uses one request ID for all three metric families.
- [x] Canonical request IDs appear as exemplars.
- [x] Invalid or missing request IDs produce ordinary samples.
- [x] `request_id` remains absent from normal metric labels.
- [x] Failure logs include the same `request_id`.
- [x] ADR-105 is indexed and CHANGELOG includes the Phase 58 entry.

---

## Non-Goals

- No trusted tenant source for Scheduler.
- No metric family or label change.
- No protocol or Worker runtime change.
- No Helm, CRD, storage, or dependency change.
- No tracing backend.

---

## Rollback

Remove the Scheduler request-ID generator, Recorder injection, and error-log
field, and restore direct writes to package-level metric vectors.
