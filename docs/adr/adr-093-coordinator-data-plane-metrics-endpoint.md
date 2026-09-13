# ADR-093: Coordinator Data-Plane Metrics Endpoint

## Status

Accepted

## Context

ADR-051 activates seven Java data-plane metrics and defines an opt-in
`METRICS_LISTEN_ADDRESS` endpoint. The Worker executable already starts
`DataPlaneMetricsServer`, but the Coordinator executable records the
Coordinator-side metric families into the same process registry without
exposing that registry.

As a result, setting `METRICS_LISTEN_ADDRESS` on a Coordinator process had no
effect. Batch-size, batch-duration, checkpoint-duration, and the shared
Worker-local spill metric could not be scraped from the process that emitted
them.

## Decision

Wire the existing `DataPlaneMetricsServer` into `CoordinatorApplication`:

1. `startMetricsServer(environment)` uses the process-local Prometheus
   registry.
2. The server starts only when `METRICS_LISTEN_ADDRESS` is non-empty.
3. The endpoint remains active for the duration of the one-shot Coordinator
   run.
4. `main` closes the server in a `finally` block after successful completion
   or failure.
5. Invalid or unusable metrics configuration follows the normal Coordinator
   startup-failure path and exits non-zero.

No new registry, metric family, port default, or HTTP implementation is
introduced. The change reuses the ADR-051 endpoint and descriptor set.

## Consequences

- Coordinator and Worker processes now have the same opt-in metrics endpoint
  contract.
- Operators can scrape Coordinator metrics while a distributed batch run is
  active.
- Because the Coordinator is one-shot, the endpoint closes when the run
  finishes; it is not a post-run historical store.
- Default-disabled deployments and Helm behavior are unchanged.

## Rollback

Remove metrics-server startup and shutdown from `CoordinatorApplication`.
Coordinator metric recording remains in memory but is no longer exposed over
HTTP.
