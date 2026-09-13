# ADR-106: Console Request ID Exemplars

## Status

Accepted

## Context

The Console BFF exposes `console_request_total` and
`console_render_duration_seconds`. Its request metrics Recorder could not
accept a request ID, and the Console `/metrics` handler did not enable
OpenMetrics negotiation.

The BFF previously assigned `X-Request-ID` inside the downstream backend
context builder. That happened after the outer request-observation middleware,
so the metric call site could not use the same identity. Requests without an
incoming header could also generate different IDs at different backend call
sites, and the auth flow did not share a request-boundary ID with metrics.

## Decision

1. The outer Console BFF middleware assigns exactly one request ID per HTTP
   request. It trims and reuses a non-empty `X-Request-ID` of at most 128
   bytes; otherwise it generates a UUID.
2. The middleware writes the selected value back to the request header and
   request context. Downstream gRPC metadata and auth-flow calls reuse that
   same value.
3. `console_request_total` and `console_render_duration_seconds` use the same
   request ID for all samples produced by the request.
4. `request_id` is attached as an exemplar only when the value is a canonical
   lowercase UUID. Empty, non-UUID, uppercase, compact, URN, and oversized
   values still produce ordinary metric samples without an exemplar.
5. The Console `/metrics` handler enables OpenMetrics negotiation because the
   legacy Prometheus text format cannot transmit exemplars.
6. `request_id` remains absent from normal metric labels and does not widen
   time-series cardinality.

The change is transport- and deployment-neutral. It does not add a protocol
field, metric family, normal label, dependency, Helm value, or CRD change.

## Consequences

- Operators can join Console request and render metric samples to the
  corresponding BFF request, downstream control-plane request, and auth flow
  using one canonical `request_id`.
- Existing Console metric names, normal labels, outcomes, and Prometheus text
  behavior remain unchanged when OpenMetrics is not requested.
- Caller-controlled or malformed header values remain bounded because they are
  never copied into normal labels and are not emitted as exemplars.
- Tests can inject an isolated metric recorder and assert the request boundary
  uses one deterministic ID.

## Rollback

Remove the request-boundary ID assignment and propagation, restore ID
generation in the backend context builder, remove the request-ID argument from
the Console Recorder, and restore the Console `/metrics` handler's default
content negotiation. Existing samples remain otherwise unchanged.
