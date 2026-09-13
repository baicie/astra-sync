# ADR-103: Java Data-Plane Request ID Exemplars

## Status

Accepted

## Context

ADR-047 defines a bounded exemplar contract with one `request_id` label.
ADR-051 selected Micrometer for Java data-plane metrics, ADR-095 added
OpenMetrics content negotiation, and ADR-102 carries one canonical request ID
through normal and checkpoint Worker execution.

Micrometer's public `Counter`, `Timer`, and `DistributionSummary` APIs do not
expose custom exemplar labels. The underlying Prometheus client does expose
`incWithExemplar` and `observeWithExemplar`, but its custom exemplar sampler
loads `io.prometheus.metrics.tracer.initializer.SpanContextSupplier` from the
`prometheus-metrics-tracer-initializer` runtime module.

## Decision

1. Add request-aware overloads to `DataPlaneMetrics` for the seven Java
   data-plane families. Existing overloads remain compatible and pass no
   request ID.
2. When the supplied registry is a `PrometheusMeterRegistry`, register the
   seven families directly in its underlying Prometheus registry and use the
   Prometheus client exemplar APIs. Other `MeterRegistry` implementations
   retain the existing Micrometer behavior.
3. Attach an exemplar only when the request ID is a canonical lowercase UUID.
   Blank, `_unknown`, uppercase, compact, URN, and arbitrary values produce
   the normal sample without an exemplar.
4. Keep `request_id` out of the normal metric label set. It must not widen
   time-series cardinality.
5. Pass trusted tenant, job, and request identities from
   `CheckpointBatchCoordinator` and `InProcessBatchWorker` into all seven
   families.
6. Add `io.prometheus:prometheus-metrics-tracer-initializer` as a runtime
   dependency at the Prometheus client version selected by Micrometer. This
   module supplies the no-op span context used when no tracing backend is
   configured. Exclude its OpenTelemetry tracer and agent adapters so this
   phase does not add a tracing implementation.
7. Emit exemplars only in the OpenMetrics representation. The Prometheus text
   `0.0.4` representation remains compatible and contains no exemplar syntax.

The protocol, metric names, normal labels, and metric types do not change.

## Consequences

- Operators can move from a data-plane metric spike to the matching
  Coordinator/Worker log records using `request_id`.
- Data-plane metrics retain bounded time-series cardinality because request
  IDs remain exemplar-only.
- Old requests without a request ID continue to emit ordinary samples.
- The Java data plane uses the Prometheus client selected by
  `micrometer-registry-prometheus`; no second metrics implementation is
  exposed through HTTP.
- Prometheus exemplar sampling and retention use the client defaults.
- No Helm, CRD, state backend, protocol version, or metric schema change is
  required.

## Rollback

Remove the request-aware overloads, restore all calls to the Micrometer-only
methods, and remove the Prometheus initializer runtime dependency. Existing
data-plane metric names, labels, samples, and request-ID log propagation
remain unchanged.
