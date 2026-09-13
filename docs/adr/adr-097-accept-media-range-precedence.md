# ADR-097: Accept Media Range Precedence

## Status

Accepted

## Context

ADR-096 compares valid quality values for exact `application/openmetrics-text`
and `text/plain` media ranges. HTTP `Accept` also permits subtype wildcards
(`text/*`) and a global wildcard (`*/*`).

Ignoring wildcard ranges can select the wrong representation:

```text
Accept: application/openmetrics-text;q=0.5, text/*;q=1
```

Here `text/*` matches `text/plain` with a higher quality than the explicit
OpenMetrics range. Selecting OpenMetrics would ignore the negotiated
preference.

## Decision

Resolve the effective quality for each supported representation using media
range specificity:

1. An exact media range has the highest precedence.
2. A type wildcard such as `text/*` has the next precedence.
3. The global wildcard `*/*` has the lowest precedence.
4. At the same specificity, use the highest valid quality value.
5. Invalid quality values make that range unavailable; a less specific valid
   range may then apply.
6. Apply wildcard ranges when calculating the effective Prometheus text
   quality.
7. Continue to require an exact `application/openmetrics-text` range to opt
   into OpenMetrics; `application/*` and `*/*` do not select it.

OpenMetrics is selected only when its effective quality is greater than the
effective Prometheus text quality. Ties and absent matches retain the
Prometheus text default.

## Consequences

- Exact, type-wildcard, and global-wildcard preferences are resolved with the
  HTTP specificity ordering.
- A lower-quality exact `text/plain` range is not overridden by a higher
  global wildcard.
- Wildcards can express a Prometheus preference but cannot implicitly enable
  OpenMetrics.
- The response content type, `Vary: Accept`, and fallback defaults from
  ADR-095 and ADR-096 remain unchanged.
- No metric family, label, protocol, deployment, or dependency change is
  required.

## Rollback

Restore exact-media-type-only quality resolution from ADR-096. OpenMetrics and
Prometheus response bodies remain otherwise unchanged.
