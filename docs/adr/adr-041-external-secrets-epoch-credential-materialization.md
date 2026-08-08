# ADR-041: External Secret References and Epoch-scoped Credential Materialization

## Status

Accepted

## Context

`JobSpec.connection_ref` exists in protobuf and Go models, but Scheduler deliberately rejects it
because no catalog, tenant resolution, Secret provider, or runtime injection path exists. JDBC and
CDC connectors still accept usernames and passwords in raw option maps. Persisting those values in
JobSpecs or PostgreSQL would expose them through APIs, backups, logs, audit, idempotency, and
Kubernetes resources.

Resolving a mutable Secret alias at every retry would create a different problem: rotation could
silently change an already accepted execution, Scheduler retry could use different credentials,
and stale epochs could escape their fence. Resolving credentials during canonical validation would
also add network I/O and secret authority to a boundary intentionally designed to be side-effect
free.

## Decision

Store tenant-scoped Connection identity, non-secret settings, and a restricted external Secret
locator in PostgreSQL, but never Secret bytes. Resolve the human `connection_ref` during Job
mutation to a stable same-tenant Connection UID. On each changed Start, atomically capture the
current immutable Connection generation for source and sink in the new execution epoch.

Require external Secret objects to be immutable or exactly version-addressable. The first adapter
uses a tenant-namespace-mapped Kubernetes Secret pinned by name and UID with `immutable: true`.
Rotation references a newly provisioned object and appends a Connection generation. It affects
epochs accepted afterward; running or already accepted epochs keep their captured generation.
Disable blocks future bindings and Starts but does not implicitly stop accepted work.

After Scheduler owns the dispatch lease, use a dedicated service identity to resolve only the
captured locator and create a deterministic immutable credential Secret scoped to Job UID and
epoch. Mount a strict envelope as read-only files into that epoch's data-plane pods. Do not put
credentials in JobSpec, environment variables, command arguments, labels, annotations, logs,
traces, audit, idempotency records, or public responses. Runtime merges descriptor defaults,
captured Connection values, secret fields, and disjoint Job-owned options before connector
creation, then applies the existing artifact fence.

Keep canonical validation metadata-only; it receives non-secret values and secret-field presence.
Implement connectivity testing as a separately permissioned, rate-limited, isolated, read-only
operation. External provider Secrets remain externally owned and are never deleted by AstraSync.
Remove Scheduler's current `connectionRef` rejection only after end-to-end authorization,
materialization, redaction, retry, race, and cleanup gates pass.

## Consequences

- Secret bytes stay outside control-plane persistence and ordinary API/telemetry surfaces.
- Stable UID and generation bindings prevent same-name recreation, rotation, or retry from changing
  an accepted epoch.
- Rotation is deterministic but not immediate revocation; compromised running work must be stopped
  and provider credentials revoked explicitly.
- Scheduler gains provider access, execution Secret creation, receipts, cleanup, and additional
  failure modes that require strict service identity and reconciliation.
- External Secret provisioning/versioning is an operational prerequisite; AstraSync does not own
  provider credential creation or deletion.
- Start acceptance remains distinct from successful materialization and runtime convergence.
- Isolated tests can prove bounded reachability without weakening canonical validation or turning
  the API Server into a network proxy.
