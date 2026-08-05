# ADR-030: Lease-fenced Scheduler Dispatch

## Status

Accepted

## Context

ADR-029 makes PostgreSQL the desired-state authority and allocates a new execution epoch for every
accepted Start, but it intentionally stops before launching a Coordinator. Multiple Scheduler
replicas must consume that state without exceeding cluster capacity or launching overlapping
executions. The existing distributed JDBC runtime also requires Coordinator and Workers to read
the same immutable JobSpec, and its checkpoint store previously allocated an independent epoch on
every process attempt.

A process-local queue or Kubernetes Job name generated per attempt would duplicate work after a
Scheduler crash. Treating lease expiry as free capacity would likewise permit the old execution to
continue while a replacement starts. Sharing one static Worker pool would violate ADR-025 because
the current Worker task factory is compiled from one mounted JobSpec.

## Decision

Persist one dispatch row per immutable `(job UID, execution epoch)`. Serialize admission with a
PostgreSQL transaction advisory lock and count all non-terminal dispatch rows against global
capacity, including rows whose ownership lease expired. Lease expiry transfers that same row to a
new Scheduler owner; owner and unexpired lease are required for every phase or terminal update.

Derive Kubernetes resource names from UID and epoch. For each dispatch, create an immutable JobSpec
Secret, execution-scoped Worker StatefulSet and headless Service, and a one-shot Coordinator Job.
Wait for Workers before creating the Coordinator. Translate Kubernetes Job conditions into the
existing lifecycle, preserve the terminal Coordinator Job until TTL, and delete all execution
resources before confirming cancellation.

Use a stable UID-derived data-plane job ID and pass the control-plane epoch to the Coordinator.
Checkpoint storage accepts that external epoch once and reuses it idempotently for Kubernetes Job
retries; stale and skipped epochs are rejected. Keep automatic epoch allocation only for legacy and
local invocations where no external epoch is configured.

## Consequences

- Multiple Scheduler replicas can admit work concurrently without a leader while respecting one
  durable global capacity.
- A crash or timeout may repeat Kubernetes API calls, but deterministic identity makes those calls
  converge on one execution group.
- An expired execution still occupies capacity until a replica takes it over and reaches a terminal
  result; this favors correctness over availability.
- Dedicated Workers make persisted JobSpecs executable without widening the Worker protocol or
  mixing credentials between jobs, at the cost of per-execution startup latency and resources.
- The default progress PVC supports one active job. Higher concurrency requires storage that can be
  mounted by the scheduled Coordinator pods, typically RWX.
- `connectionRef` remains a rejected permanent materialization until catalog/Secret resolution is
  designed.
- Lease takeover is not full Coordinator HA. Heartbeats, orphan sweeps, controller convergence, and
  failover validation remain Phase 4 Slice 13 work.
