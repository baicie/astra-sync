# ADR-096: Quality-Weighted OpenMetrics Content Negotiation

## Status

Accepted — media range precedence refined by ADR-097.

## Context

ADR-095 added OpenMetrics representation selection to the Java data-plane
`/metrics` endpoint. Its first implementation selected OpenMetrics whenever a
specific and acceptable `application/openmetrics-text` media range appeared in
the HTTP `Accept` header.

HTTP quality values express relative client preference. A client can send:

```text
Accept: application/openmetrics-text;q=0.5, text/plain;q=1
```

The endpoint must prefer `text/plain` in that request. Selecting OpenMetrics
only because it appeared in the header would ignore the negotiated preference.

## Decision

Select the response representation from the highest valid quality value for
each supported specific media type:

1. Parse exact `application/openmetrics-text` and `text/plain` media ranges.
2. Treat an absent `q` parameter as `q=1`.
3. Use the highest valid quality value when a media type appears more than
   once.
4. Treat a missing, non-numeric, non-finite, negative, or greater-than-one
   quality value as making that media range unavailable.
5. Select OpenMetrics only when its quality is greater than the Prometheus
   text quality.
6. Select Prometheus text on a tie, when OpenMetrics is absent or rejected,
   and when no `Accept` header is supplied.
7. Wildcard media ranges do not opt into OpenMetrics; an explicit OpenMetrics
   media range remains required.

ADR-097 refines Prometheus quality resolution by applying exact, type-wildcard,
and global-wildcard precedence.

The selected response content type and `Vary: Accept` behavior remain the
contract defined by ADR-095.

## Consequences

- Clients can express a weighted preference between OpenMetrics and
  Prometheus text without the server overriding that preference.
- Existing clients that do not request OpenMetrics keep the Prometheus text
  default.
- Tie-breaking remains deterministic and favors the backward-compatible
  representation.
- Invalid quality values cannot accidentally enable OpenMetrics.
- No metric family, label, protocol, deployment, or dependency change is
  required.

## Rollback

Remove quality weighting and restore the ADR-095 behavior of selecting
OpenMetrics whenever a specific `application/openmetrics-text` media range has
a positive `q` value. The endpoint otherwise remains unchanged.
