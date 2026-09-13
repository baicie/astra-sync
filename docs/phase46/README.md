# Phase 46 - Quality-Weighted OpenMetrics Negotiation

## Status

**Complete.**

Phase 46 refines the ADR-095 OpenMetrics negotiation by honoring HTTP quality
values instead of selecting OpenMetrics on media-range presence alone.

ADR: [ADR-096](../adr/adr-096-quality-weighted-openmetrics-negotiation.md)

---

## Goal

Select the response representation from the client's relative preference:

```text
OpenMetrics q > Prometheus text q  -> OpenMetrics 1.0.0
otherwise                          -> Prometheus text 0.0.4
```

The default, content type, `# EOF`, and `Vary: Accept` contracts remain
unchanged.

---

## Delivered Files

```text
engine/src/main/java/io/astrasync/engine/observability/
└── DataPlaneMetricsServer.java

engine/src/test/java/io/astrasync/engine/observability/
└── DataPlaneMetricsServerTest.java

docs/adr/
└── adr-096-quality-weighted-openmetrics-negotiation.md

docs/phase46/
└── README.md
```

---

## Negotiation Contract

```text
application/openmetrics-text       quality defaults to 1
text/plain                         quality defaults to 1
OpenMetrics q > text/plain q       OpenMetrics
OpenMetrics q <= text/plain q      Prometheus text
```

The highest valid quality is used when an exact media type appears more than
once. Invalid quality values make that media range unavailable. Wildcards do
not select OpenMetrics; the OpenMetrics media type must be explicit.

Wildcard precedence is completed by Phase 47
([ADR-097](../adr/adr-097-accept-media-range-precedence.md)).

---

## Verification

`DataPlaneMetricsServerTest` verifies:

- blank listen address does not bind;
- the default response remains Prometheus text `0.0.4`;
- an explicit OpenMetrics media range returns OpenMetrics `1.0.0`;
- the OpenMetrics body contains `# EOF`;
- `q=0` rejects OpenMetrics;
- a higher Prometheus quality value wins;
- a higher OpenMetrics quality value wins;
- equal quality values retain the Prometheus default;
- an invalid OpenMetrics quality value retains the Prometheus default;
- responses include `Vary: Accept`.

---

## Acceptance Criteria

- [x] Exact media types are compared using their highest valid quality value.
- [x] Missing `q` defaults to quality 1.
- [x] Invalid and out-of-range quality values cannot select OpenMetrics.
- [x] Higher OpenMetrics quality selects OpenMetrics.
- [x] Higher Prometheus quality selects Prometheus text.
- [x] Ties retain the Prometheus text default.
- [x] Wildcards do not implicitly opt into OpenMetrics.
- [x] ADR-096 is indexed in `docs/adr/README.md`.
- [x] CHANGELOG includes the Phase 46 entry.

---

## Non-Goals

- No exemplar emission.
- No metric family or label change.
- No protobuf, Helm, CRD, dependency, or deployment change.

---

## Rollback

Restore presence-based selection from ADR-095. The response representations
and endpoint activation contract remain otherwise unchanged.
