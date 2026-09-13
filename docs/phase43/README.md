# Phase 43 - Coordinator Data-Plane Metrics Endpoint

## Status

**Complete.**

Phase 43 exposes the Java Coordinator's data-plane metrics through the
existing opt-in `METRICS_LISTEN_ADDRESS` contract.

ADR: [ADR-093](../adr/adr-093-coordinator-data-plane-metrics-endpoint.md)

---

## Goal

Make Coordinator and Worker executables symmetric:

- no listener when `METRICS_LISTEN_ADDRESS` is absent or blank;
- Prometheus text on `/metrics` when explicitly configured;
- bounded shutdown at the end of the Coordinator run.

---

## Delivered Files

```text
engine/coordinator/src/main/java/io/astrasync/engine/coordinator/
└── CoordinatorApplication.java

engine/coordinator/src/test/java/io/astrasync/engine/coordinator/
└── CoordinatorApplicationTest.java

docs/adr/
└── adr-093-coordinator-data-plane-metrics-endpoint.md

docs/phase43/
└── README.md
```

---

## Runtime Contract

`CoordinatorApplication.startMetricsServer`:

```text
METRICS_LISTEN_ADDRESS absent/blank -> Optional.empty()
METRICS_LISTEN_ADDRESS host:port    -> live /metrics endpoint
```

The Coordinator starts the endpoint before executing the job and closes it in
`finally` after success or failure. Invalid metrics configuration is reported
through the existing Coordinator startup-failure path.

---

## Verification

`CoordinatorApplicationTest` verifies:

- an absent setting does not bind a listener;
- `127.0.0.1:0` binds an ephemeral port;
- `/metrics` returns HTTP 200;
- the body contains a real `worker_records_read_total` sample recorded in the
  process registry.

Existing distributed Coordinator tests continue to cover the execution path.

---

## Acceptance Criteria

- [x] Coordinator exposes the existing process registry.
- [x] The endpoint is opt-in through `METRICS_LISTEN_ADDRESS`.
- [x] The endpoint closes on success and failure.
- [x] The `/metrics` response contains recorded data-plane samples.
- [x] Worker endpoint behavior is unchanged.
- [x] No Helm, protocol, persistence, or dependency change is required.
- [x] ADR-093 is indexed in `docs/adr/README.md`.
- [x] CHANGELOG includes the Phase 43 entry.

---

## Non-Goals

- No default-on metrics listener.
- No new metric family or label.
- No Protocol or tenant-identity change.
- No long-running Coordinator metrics retention.
- No Helm chart or ServiceMonitor change.

---

## Rollback

Remove endpoint startup and shutdown from `CoordinatorApplication` and the
Phase 43 documentation/index entries. Metric recording remains unchanged.
