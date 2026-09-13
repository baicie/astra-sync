# Phase 56 - Data-Plane Request ID Exemplars

## Status

**Complete.**

Phase 56 attaches canonical `request_id` exemplars to all seven Java
data-plane metric families without adding a normal metric label.

References:
[ADR-103](../adr/adr-103-java-data-plane-request-id-exemplars.md),
[ADR-102](../adr/adr-102-data-plane-request-id-propagation.md),
[ADR-095](../adr/adr-095-openmetrics-content-negotiation.md)

---

## Goal

```text
canonical request_id -> OpenMetrics sample -> request_id exemplar
missing/invalid ID  -> ordinary sample      -> no exemplar
Prometheus text     -> ordinary sample      -> no exemplar syntax
```

---

## Delivered Files

```text
pom.xml
engine/pom.xml

engine/src/main/java/io/astrasync/engine/observability/
└── DataPlaneMetrics.java

engine/coordinator/src/main/java/io/astrasync/engine/coordinator/
└── CheckpointBatchCoordinator.java

engine/worker/src/main/java/io/astrasync/engine/worker/
└── InProcessBatchWorker.java
```

---

## Exemplar Contract

All seven Java data-plane families receive the same bounded behavior:

```text
coordinator_batch_size_records
coordinator_batch_duration_seconds
coordinator_spill_bytes_total
coordinator_checkpoint_duration_seconds
worker_records_read_total
worker_records_written_total
worker_records_rejected_total
```

Only a canonical lowercase UUID is accepted. The exemplar label allowlist
contains one entry, `request_id`. The value is never copied into the normal
series labels.

The Prometheus registry path uses the Prometheus client APIs directly so the
custom exemplar label can be preserved. Other registries continue to use the
existing Micrometer behavior.

---

## Verification

- `DataPlaneMetricsTest` verifies all seven families emit canonical
  `request_id` exemplars and that invalid IDs produce ordinary samples.
- `InProcessBatchWorkerTest` verifies worker read/write and batch-duration
  exemplars.
- `CheckpointBatchCoordinatorMetricsTest` verifies batch-size, batch-duration,
  and checkpoint-duration exemplars.
- `CoordinatorApplicationTest` verifies the process registry exposes the
  OpenMetrics sample after a real Coordinator run.
- `DataPlaneMetricsServerTest` verifies Prometheus text and OpenMetrics
  content negotiation remain unchanged.

---

## Acceptance Criteria

- [x] All seven Java data-plane families attach canonical request-ID
  exemplars.
- [x] Invalid, missing, and unknown request IDs emit ordinary samples.
- [x] `request_id` is absent from normal metric labels.
- [x] Prometheus text output remains compatible.
- [x] Coordinator checkpoint and Worker callsites propagate the request ID.
- [x] The required Prometheus initializer runtime dependency is declared in
  the root dependency management and engine module, with tracing adapters
  excluded.
- [x] ADR-103 is indexed and CHANGELOG includes the Phase 56 entry.

---

## Non-Goals

- No metric family, label, or bucket boundary change.
- No protocol change.
- No tracing backend or OpenTelemetry propagation.
- No Helm, CRD, state backend, or checkpoint change.
- No audit-table write change.

---

## Rollback

Remove the request-aware metric overloads and the Prometheus initializer
runtime dependency. Existing samples and log correlation remain otherwise
unchanged.
