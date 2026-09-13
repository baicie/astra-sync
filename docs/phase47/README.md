# Phase 47 - Accept Media Range Precedence

## Status

**Complete.**

Phase 47 completes the HTTP media range matching used by OpenMetrics
negotiation. Quality values now resolve through exact, type-wildcard, and
global-wildcard precedence instead of exact media types alone.

ADR: [ADR-097](../adr/adr-097-accept-media-range-precedence.md)

---

## Goal

Select the metrics representation from the client's most specific matching
media ranges:

```text
exact text/plain       > text/*       > */*
exact OpenMetrics      is required to opt into OpenMetrics
```

The Prometheus text default, OpenMetrics `1.0.0` content type, `# EOF`, and
`Vary: Accept` contracts remain unchanged.

---

## Delivered Files

```text
engine/src/main/java/io/astrasync/engine/observability/
└── DataPlaneMetricsServer.java

engine/src/test/java/io/astrasync/engine/observability/
└── DataPlaneMetricsServerTest.java

docs/adr/
└── adr-097-accept-media-range-precedence.md

docs/phase47/
└── README.md
```

---

## Negotiation Contract

```text
application/openmetrics-text;q=0.5, text/*;q=1
  -> Prometheus text

application/openmetrics-text;q=0.8, text/plain;q=0.5, */*;q=1
  -> OpenMetrics (exact text/plain quality is 0.5)

application/openmetrics-text;q=1, */*;q=0.5
  -> OpenMetrics
```

The effective quality comes from the most specific valid match. At equal
specificity, the highest valid quality wins. Invalid ranges are ignored for
that match. OpenMetrics still requires an exact media range.

---

## Verification

`DataPlaneMetricsServerTest` verifies:

- default Prometheus text behavior;
- explicit OpenMetrics negotiation and `# EOF`;
- `q=0` rejection and invalid quality fallback;
- higher Prometheus and OpenMetrics quality values;
- equal quality tie fallback;
- `text/*` can override a lower OpenMetrics quality;
- an exact `text/plain` quality takes precedence over `*/*`;
- a lower `*/*` quality does not override explicit OpenMetrics;
- responses include `Vary: Accept`.

---

## Acceptance Criteria

- [x] Exact media ranges outrank type and global wildcards.
- [x] Type wildcards outrank the global wildcard.
- [x] The highest valid quality is used at the same specificity.
- [x] Invalid quality values do not enable a media range.
- [x] Wildcards can express a Prometheus preference.
- [x] Wildcards do not implicitly opt into OpenMetrics.
- [x] ADR-097 is indexed in `docs/adr/README.md`.
- [x] CHANGELOG includes the Phase 47 entry.

---

## Non-Goals

- No 406 response contract.
- No exemplar emission.
- No metric family, label, protobuf, Helm, CRD, dependency, or deployment
  change.

---

## Rollback

Restore exact-media-type-only quality resolution from Phase 46. The endpoint
activation and response representations remain otherwise unchanged.
