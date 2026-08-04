# ADR-025: Distributed JDBC Operational Slice

## Status

Accepted

## Context

Phase 1 has a split scheduler, remote Worker protocol, JDBC range enumeration, and durable
split-level progress, but the pieces are not yet connected by a production process. Slice 04 leaves
task materialization behind a deployment plugin contract and has no Coordinator entry point. Its
Worker Deployment also gives pods replaceable names, while resumable records and protocol requests
need stable Worker identities.

The operational path must preserve the existing ownership boundary: a Coordinator may enumerate and
schedule split descriptors, but JDBC connections, Sources, and Sinks belong to Workers. It must also
retain the Phase 1 at-most-once boundary rather than presenting split progress as an exactly-once
checkpoint.

## Decision

1. The Coordinator and every Worker read the same immutable JobSpec file. The Coordinator compiles
   it, requires JDBC source and sink connectors, and uses source options only to enumerate the full
   JDBC range plan. Each Worker compiles the same document and creates fresh JDBC Source and Sink
   resources for an assigned split.
2. Add `RemoteTaskFactory` for descriptor-only Coordinator tasks. Its placeholder resources fail if
   opened locally, making an ownership violation explicit. The Worker protocol continues to carry
   only the split descriptor and bounded execution limits.
3. Add a production `JdbcWorkerTaskFactoryProvider` to the standard shaded Worker JAR. The image
   contains this provider and the supported MySQL and PostgreSQL JDBC drivers; no external plugin is
   required for the JDBC path.
4. Configure Workers as a StatefulSet behind a headless Service. A Worker endpoint uses
   `worker-id@host:port`, where the Worker ID is the stable pod name and the host is that pod's stable
   StatefulSet DNS name. Duplicate Worker IDs are rejected before execution.
5. Run the Coordinator as a one-shot Kubernetes Job because it exits after success or failure. The
   Job and StatefulSet mount one existing JobSpec Secret. The Coordinator also requires an existing
   persistent volume claim for progress; a failed Job attempt reopens that volume and schedules only
   unfinished splits.
6. Keep one active Coordinator per job ID. Worker discovery, retries within an invocation, leader
   election, epoch fencing, TLS, and authentication remain outside this slice.

## Consequences

A JDBC full load can now run through two or more real TCP Workers from a packaged Coordinator, and a
new invocation can resume durable split completions. Stateful Worker names make protocol identities
and generated endpoints deterministic across pod replacement. Docker Compose provides the same
two-Worker path for local verification.

Operators must distribute one immutable JobSpec, keep its credentials in a Secret, and retain a
single-writer progress volume. Changing the enumerated split IDs or boundaries under the same job ID
is rejected. A crash after a Worker commits sink data but before the Coordinator replaces the
manifest can replay that split. Failed splits also restart from their original range boundary, so
delivery remains at-most-once at the runtime contract and duplicate-sensitive sinks still require
operator-provided idempotency.
