# ADR-024: Split-level Resumable Full-load Execution

## Status

Accepted

## Context

Phase 1 can enumerate stable source splits and execute them on remote Workers, but a failed
Coordinator invocation forgets every successful task. Re-running the full split set wastes work and
can duplicate sink effects. A full consistency checkpoint as described by ADR-005 still requires
source snapshots, coordinated sink commits, and epoch fencing, none of which exist in Phase 1.

The network Worker also lacks a process entry point and a deployable runtime contract. The existing
container and Helm probes describe HTTP endpoints that the Worker does not implement.

## Decision

1. Define a deterministic `SplitPlan` from the complete enumerated split set. The plan and each split
   are fingerprinted with SHA-256 over canonical split ID, source ID, start offsets, and end offsets.
2. Persist one versioned JSON manifest per job ID. The file store takes an exclusive lock, writes a
   forced temporary file in the same directory, and requires atomic replacement. A durable volume
   that lacks file locks or atomic rename is unsupported.
3. Record only successful split results. The first durable success for a split wins; later duplicate
   completions cannot replace its Worker ID or metrics. A changed plan or split descriptor is rejected
   before unfinished tasks are materialized.
4. `ResumableBatchCoordinator` opens the manifest before scheduling, skips completed splits, and
   persists new results in task-completion order. It does not retry a failed task in the same
   invocation. A later invocation with the same job ID and plan starts each unfinished split again at
   its original split boundary.
5. Limit the store to one active Coordinator per job. File locking protects manifest replacement, but
   Phase 1 does not add leader election, Coordinator epochs, distributed fencing, or concurrent-writer
   correctness.
6. Package the Worker as an executable shaded JAR. A deployment-supplied class implementing
   `WorkerTaskFactoryProvider` creates Worker-local task resources from process configuration. The
   Worker exposes the existing TCP protocol port for execution and health checks and fails fast when
   the provider is missing or invalid.
7. Keep Helm Worker deployment disabled by default. Enabling it requires a provider class and an
   image that places the provider and its dependencies on the Worker plugin classpath.

## Consequences

Completed split work survives a normal Coordinator failure or restart, and unchanged jobs avoid
materializing completed split resources. Operators must retain the manifest on durable storage and
reuse a job ID only with the identical enumerated plan. Starting a deliberately changed plan requires
a new job ID or an explicit out-of-band removal of the old manifest.

This is split-level progress, not exactly-once checkpointing. If a sink succeeds and the process dies
before the success reaches the atomic manifest, that split runs again and may duplicate effects. A
failed split also restarts from its original boundary rather than an intra-split offset. Exactly-once
still requires replayable sources, coordinated transactional or idempotent sinks, and epoch fencing.
