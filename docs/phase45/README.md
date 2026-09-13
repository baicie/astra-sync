# Phase 45 - Data-Plane OpenMetrics Negotiation

## Status

**Complete.**

Phase 45 closes the OpenMetrics content-negotiation deferral recorded by
ADR-051 without changing the metrics catalog or deployment contract.

ADR: [ADR-095](../adr/adr-095-openmetrics-content-negotiation.md)

---

## Goal

Allow Prometheus clients to request OpenMetrics through the standard
`Accept` header while preserving Prometheus text `0.0.4` as the default.

---

## Delivered Files

```text
engine/src/main/java/io/astrasync/engine/observability/
└── DataPlaneMetricsServer.java

engine/src/test/java/io/astrasync/engine/observability/
└── DataPlaneMetricsServerTest.java

docs/adr/
└── adr-095-openmetrics-content-negotiation.md

docs/phase45/
└── README.md
```

---

## Negotiation Contract

```text
Accept contains application/openmetrics-text
  -> application/openmetrics-text; version=1.0.0; charset=utf-8

otherwise
  -> text/plain; version=0.0.4; charset=utf-8
```

The response includes `Vary: Accept`. An explicit `q=0` for the OpenMetrics
media type does not select OpenMetrics.

---

## Verification

`DataPlaneMetricsServerTest` verifies:

- blank listen address does not bind;
- the default response remains Prometheus text `0.0.4`;
- an OpenMetrics `Accept` value returns the OpenMetrics content type;
- the OpenMetrics body contains the `# EOF` terminator;
- `q=0` explicitly rejects OpenMetrics and keeps Prometheus text;
- the response includes `Vary: Accept`.

---

## Acceptance Criteria

- [x] Default Prometheus text response is unchanged.
- [x] Explicit OpenMetrics negotiation is supported.
- [x] The selected content type drives the registry writer.
- [x] `Vary: Accept` is emitted.
- [x] No exemplar claim is made before exemplar emission is implemented.
- [x] ADR-095 is indexed in `docs/adr/README.md`.
- [x] CHANGELOG includes the Phase 45 entry.

---

## Non-Goals

- No exemplar emission or request-id propagation.
- No new metric family or label.
- No Helm, ServiceMonitor, protocol, or dependency change.

---

## Rollback

Remove negotiation and always return Prometheus text `0.0.4`.
