# ADR-105: Scheduler Request ID Exemplars

## Status

Accepted

## Context

The Scheduler exposes assignment, lease-takeover, and reconcile-duration
families. Its Recorder already accepted a `request_id` argument for the first
two families, but production ignored it and wrote directly to process-global
CounterVec and HistogramVec values. Reconcile duration had no request ID at
all, and scheduler failure logs could not be joined to the metric samples.

The Scheduler does not receive a request ID from an external API boundary. It
can generate one request ID for each reconciliation tick and use that identity
consistently across all observations produced by the tick.

## Decision

1. Generate one canonical UUID for each `Tick` invocation.
2. Use that request ID for every lease-takeover, assignment, and
   reconcile-duration observation from the tick.
3. Include the same request ID in Scheduler reconciliation failure logs.
4. Attach `request_id` as an exemplar only when it is a canonical lowercase
   UUID. The value remains absent from normal metric labels.
5. Route production scheduling metrics through the injected `metrics.Recorder`
   instead of direct package-level Vec writes. The default Recorder preserves
   the existing process-global `/metrics` surface.
6. Allow tests and embedding callers to inject a Recorder and UUID source
   through functional options.
7. Keep Scheduler tenant attribution at `_unknown`; this ADR does not add a
   trusted tenant source.

The change is transport- and deployment-neutral.

## Consequences

- Operators can join Scheduler metric spikes to the failure logs from the same
  tick using `request_id`.
- Existing Scheduler metric names, normal labels, outcomes, and global scrape
  behavior remain unchanged.
- Request IDs do not widen time-series cardinality.
- Tests can isolate Scheduler metrics and deterministic request IDs without
  using the process-global registry.
- No protocol, Helm, CRD, storage backend, or dependency change is required.

## Rollback

Remove the Scheduler UUID source and Recorder injection, restore direct writes
to the package-level metric vectors, and remove `request_id` from Scheduler
failure logs. Existing samples remain otherwise unchanged.
