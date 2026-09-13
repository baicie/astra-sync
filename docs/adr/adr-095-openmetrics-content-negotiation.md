# ADR-095: Data-Plane OpenMetrics Content Negotiation

## Status

Accepted — quality weighting refined by ADR-096; exemplar emission added by
ADR-103.

## Context

ADR-047 defines bounded `request_id` exemplars, and ADR-051 intentionally
deferred OpenMetrics content negotiation until the registry endpoint needed a
transport capable of carrying them. The Java data-plane `/metrics` endpoint
currently always returns Prometheus text format `0.0.4`.

Prometheus scrapers may advertise OpenMetrics support through the HTTP
`Accept` header. Without negotiation, exemplar-compatible clients cannot
receive the OpenMetrics representation even though Micrometer and the bundled
Prometheus client already support it.

## Decision

Make the existing `DataPlaneMetricsServer` negotiate the response format:

- no matching `Accept` value returns the existing
  `text/plain; version=0.0.4; charset=utf-8` response;
- an explicit acceptable `application/openmetrics-text` value returns
  `application/openmetrics-text; version=1.0.0; charset=utf-8`;
- quality values with `q=0` reject OpenMetrics for that media range;
- the response includes `Vary: Accept`;
- the registry writer receives the selected content type, so the body includes
  the OpenMetrics `# EOF` terminator.

ADR-096 refines selection when multiple media ranges have different `q`
values.

No exemplar is emitted in this phase. Negotiation only prepares the transport
and keeps the exemplar contract governed by ADR-047 and ADR-051.

## Consequences

- OpenMetrics-capable scrapers can select the standard representation.
- Existing scrapers that do not request OpenMetrics keep the exact Prometheus
  text behavior.
- The server response is cache-correct through `Vary: Accept`.
- The endpoint remains default-disabled through `METRICS_LISTEN_ADDRESS`.
- No metric family, label, Helm value, protocol field, or dependency changes.

## Rollback

Remove content negotiation and always scrape Prometheus text format. The
endpoint remains otherwise unchanged.
