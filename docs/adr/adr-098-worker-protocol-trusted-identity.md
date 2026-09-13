# ADR-098: Worker Protocol Trusted Identity Propagation

## Status

Accepted

## Context

The Java data-plane metrics contract uses `tenant_id` and `job_id` labels, but
the Worker protocol did not carry tenant identity. `ExecuteTaskRequest` also
did not carry `job_id`; only `ExecuteCheckpointTaskRequest` did. As a result,
non-checkpoint Worker metrics always used `_unknown`, and checkpoint metrics
could not attribute samples to a tenant.

ADR-023 defines the Worker protocol as a versioned, append-only wire contract.
The missing identity must be added without changing existing field numbers,
altering request dispatch, or requiring a protocol-version break.

## Decision

Append optional identity fields to the existing Worker request messages:

1. Add `job_id = 8` and `tenant_id = 9` to `ExecuteTaskRequest`.
2. Add `tenant_id = 14` to `ExecuteCheckpointTaskRequest`, where `job_id`
   already exists at field 2.
3. Keep protocol version `1` for normal tasks and version `2` for checkpoint
   tasks; the new fields are wire-compatible protobuf additions.
4. Treat empty or blank identity fields from older Coordinators as the
   bounded `_unknown` value.
5. Rebind the materialized `BatchTask` to the identity carried by the
   authenticated Coordinator-to-Worker request before execution.
6. Use `task.tenantId()` and `task.jobId()` for non-checkpoint Worker metrics;
   checkpoint metrics use the request-backed task tenant and the validated
   checkpoint job identity.
7. Normalize metric labels to canonical lowercase UUIDs. Non-canonical values
   collapse to `_unknown`.

The identity fields are attribution metadata. They do not grant authorization
and do not replace the existing Worker, task, epoch, or split validation.

## Consequences

- Worker metrics can carry trusted `job_id` and `tenant_id` values when the
  Coordinator supplies them.
- Existing Coordinators remain compatible because omitted protobuf fields
  decode as empty strings and normalize to `_unknown`.
- The current Java Coordinator has a trusted job name source from `JobSpec`
  metadata but no trusted tenant source in `JobSpec`; it therefore continues
  to send `_unknown` for tenant until a separate trusted tenant-binding slice
  supplies one.
- The protocol remains version-compatible and no generated Go contract changes
  are required.
- No metric family, label name, deployment value, or storage backend changes.

## Rollback

Remove the appended protobuf fields and restore Worker metrics to fixed
`_unknown` labels. Existing protocol versions and task execution remain
otherwise unchanged.
