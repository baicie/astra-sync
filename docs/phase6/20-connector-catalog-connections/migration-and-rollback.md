# Phase 6 Slice 20 Migration and Rollback Runbook

## Status

Implementation complete. Production rollout is an explicit operator action; all Connection write,
test, and runtime gates remain disabled by default.

## Scope and Safety Boundary

This runbook migrates persisted Jobs from inline connector-owned or sensitive options to
tenant-scoped Connections backed by immutable Kubernetes Secrets. It does not extract credentials,
create provider Secrets from database values, or write plaintext credentials to files, shell
history, logs, audit events, or browser storage.

Use an approved secret-provisioning workflow to create provider Secrets. Do not use an automated
database-to-Secret export, a generated plaintext manifest, or a command-line literal containing a
credential. The historical Connection CRD is deprecated and is not a migration source.

## Rollout Gates

| Helm value | Environment variable | Default | Effect when disabled |
|---|---|---|---|
| `apiServer.connectionRollout.mutationsEnabled` | `CONNECTION_MUTATIONS_ENABLED` | `false` | Connection create/update/rotate/enable/disable/delete is rejected; reads remain available |
| `apiServer.connectionRollout.testsEnabled` | `CONNECTION_TESTS_ENABLED` | `false` | New tests are rejected; admitted tests may drain |
| `apiServer.connectionRollout.runtimeEnabled` | `CONNECTION_RUNTIME_ENABLED` | `false` | Start of a Job containing `connection_ref` is rejected |
| `scheduler.connectionMaterialization.enabled` | `SCHEDULER_CONNECTION_MATERIALIZATION_ENABLED` | `false` | Scheduler does not resolve or create execution credential Secrets |
| `connectionTestExecutor.enabled` | n/a | `false` | The isolated test queue has no consumer |

Helm rejects runtime admission without Scheduler materialization and rejects test admission without
the executor. Catalog and Connection read paths are not disabled by rollback.

## Preflight

1. Back up PostgreSQL and record the release image digests. Confirm the backup retention policy
   covers the rollback window.
2. Verify the candidate compiler, API, Coordinator, and Worker images carry the same deployment
   inventory:

   ```sh
   make catalog-check
   ```

3. Deploy the additive migrations and candidate workloads with every gate above set to `false`.
   API startup applies auth, catalog, Connection, and Job mutation migrations before serving.
4. Confirm the active catalog revision and schema objects without reading secret-bearing columns:

   ```sql
   SELECT execution_profile, inventory_revision, activated_at
     FROM astrasync_connector_inventory_activation
    ORDER BY execution_profile;

   SELECT to_regclass('astrasync_connections') AS connections,
          to_regclass('astrasync_connection_generations') AS generations,
          to_regclass('astrasync_job_connection_bindings') AS job_bindings,
          to_regclass('astrasync_execution_connection_bindings') AS execution_bindings,
          to_regclass('astrasync_connection_materialization_receipts') AS receipts;
   ```

5. Confirm Catalog list/get and existing Connection list/get requests are authorized and redacted.
   Do not select `restricted_locator`, `encrypted_tokens`, idempotency projections, or request
   bodies for rollout diagnostics.

## Inventory Persisted Job Options

Run this query per rollout cohort. It enumerates keys and lengths only; it intentionally never
selects option values. Compare every key with the active descriptor's owner and sensitivity. Block
the tenant when a key is unknown, belongs to `CONNECTION`, is sensitive, or looks credential-like.

```sql
WITH endpoints AS (
    SELECT tenant.tenant_id,
           jobs.namespace,
           jobs.name AS job_name,
           jobs.uid AS job_uid,
           'SOURCE'::text AS role,
           jobs.spec -> 'source' AS endpoint
      FROM astrasync_control_jobs AS jobs
      JOIN astrasync_auth_tenants AS tenant USING (namespace)
    UNION ALL
    SELECT tenant.tenant_id,
           jobs.namespace,
           jobs.name,
           jobs.uid,
           'SINK'::text,
           jobs.spec -> 'sink'
      FROM astrasync_control_jobs AS jobs
      JOIN astrasync_auth_tenants AS tenant USING (namespace)
), option_inventory AS (
    SELECT endpoint.tenant_id,
           endpoint.namespace,
           endpoint.job_name,
           endpoint.job_uid,
           endpoint.role,
           endpoint.endpoint ->> 'connector' AS connector,
           NULLIF(endpoint.endpoint ->> 'connectionRef', '') AS connection_ref,
           option.key AS option_key,
           length(option.value) AS value_length,
           option.key ~* '(password|passwd|secret|token|credential|private[._-]?key|access[._-]?key|username|user)' AS suspicious_key
      FROM endpoints AS endpoint
      LEFT JOIN LATERAL jsonb_each_text(
          COALESCE(endpoint.endpoint -> 'options', '{}'::jsonb)
      ) AS option ON TRUE
)
SELECT tenant_id,
       namespace,
       job_name,
       job_uid,
       role,
       connector,
       connection_ref,
       option_key,
       value_length,
       suspicious_key
  FROM option_inventory
 ORDER BY tenant_id, namespace, job_name, role, option_key NULLS FIRST;
```

The regular expression is only a conservative tripwire; a non-matching key is not proof that the
option is safe. The active descriptor is authoritative. Investigate legacy records through an
approved, access-controlled process that does not print or stage raw values.

## Provision Immutable Provider Secrets

For each Connection generation, provision one Kubernetes Secret through the deployment's approved
secret manager or GitOps secret-encryption workflow. The resolved Secret must satisfy all of these
conditions:

- namespace is the server-owned namespace mapped from the tenant UUID;
- label `sync.astrasync.io/tenant-id` equals that tenant UUID;
- `immutable: true` is present;
- the object name and Kubernetes UID exactly match the write-only Connection locator;
- `data` contains every declared mapped key exactly once and no additional keys;
- each value is non-empty and within the descriptor and provider size bounds.

Record the Secret name and UID in the restricted migration work item, not in application logs or a
general-purpose ticket. Read metadata without revealing `data`:

```sh
kubectl get secret <secret-name> -n <tenant-namespace> \
  -o jsonpath='{.metadata.uid}{"\n"}{.immutable}{"\n"}{.metadata.labels.sync\.astrasync\.io/tenant-id}{"\n"}'
```

Deleting and recreating the same name creates a different UID and is intentionally rejected. A
credential rotation therefore provisions a new immutable Secret and calls `RotateConnection` with
the new pinned UID.

## Tenant Migration

Keep `apiServer.connectionRollout.runtimeEnabled=false` throughout migration so no
Connection-backed Start can be admitted.

1. Enable Connection mutations for the staging tenant's maintenance window. Create each
   Connection with an idempotency key. Creation always returns `DISABLED`, version 1, generation 1,
   and a redacted response.
2. With the Connection still disabled, inspect its connector, schema compatibility,
   `secret_configured`, public settings, version, and generation. If test rollout is enabled, submit
   a bounded `TestConnection` using the current expected version and wait for its redacted terminal
   result. Testing never enables the Connection.
3. Resolve any failed test or descriptor mismatch by rotating or updating while disabled. Repeat
   until the intended generation is compatible. Never patch the external Secret in place.
4. Enable the Connection with its current positive `expected_version` and a fresh idempotency key.
   Runtime admission is still closed, so enabling cannot start a Job.
5. Reload each Job, retain its returned version, remove all Connection-owned and sensitive options,
   set the source and/or sink `connection_ref`, and call `UpdateJob` with that exact
   `expected_version` plus an idempotency key. The mutation performs canonical validation and
   atomically writes stable Job-to-Connection UID bindings, the Job, idempotency result, and audit.
6. Call `ValidateJobSpec` against the stored redacted specification and confirm the active compiler
   and descriptor revisions. On a CAS conflict, reload and deliberately reapply the migration; do
   not increment a guessed version or reuse an idempotency key for a changed request.
7. Re-run the option inventory and sentinel scans. Require zero unknown, Connection-owned, or
   sensitive Job options before the tenant proceeds.

## Enable Tests and Runtime

Roll out one stage at a time and stop expansion on any unexplained denial, revision skew, audit
failure, receipt conflict, or cleanup backlog.

1. Catalog/read-only: all gates `false`; compare inventory revisions across eligible images.
2. Administration: set `apiServer.connectionRollout.mutationsEnabled=true` for controlled
   Connection CRUD while runtime use remains off.
3. Isolated tests: deploy `connectionTestExecutor.enabled=true`, verify its dedicated namespace,
   service account, quota, and NetworkPolicy, then set
   `apiServer.connectionRollout.testsEnabled=true`.
4. Runtime: in one Helm release, set both
   `scheduler.connectionMaterialization.enabled=true` and
   `apiServer.connectionRollout.runtimeEnabled=true`. Keep mutation and test gates independently
   controlled.
5. Start one migrated staging Job. Confirm one immutable execution Secret, source/sink receipt rows,
   fixed generation bindings, normal runtime load, terminal cleanup, and no locator/value sentinel
   on public surfaces before expanding to another tenant.

Observe at least:

- active catalog/inventory and compiler revision skew;
- Connection version/generation conflicts, incompatible references, and cross-tenant denials;
- queued/running/failed test counts and policy-denied/timeout result codes;
- execution binding counts, materialization receipt latency/failures, and receipt conflicts;
- pending cleanup obligations and execution credential Secrets after terminal Jobs;
- failed audit writes and sentinel scans of API responses, Console output, logs, audit attributes,
  Kubernetes metadata/events, and database projections.

Use bounded aggregate queries that do not select locators or values:

```sql
SELECT state, count(*) FROM astrasync_connections GROUP BY state ORDER BY state;
SELECT state, result_code, count(*)
  FROM astrasync_connection_tests
 GROUP BY state, result_code
 ORDER BY state, result_code;
SELECT count(*) AS execution_bindings FROM astrasync_execution_connection_bindings;
SELECT count(*) AS materialization_receipts FROM astrasync_connection_materialization_receipts;
SELECT state, count(*)
  FROM astrasync_connection_cleanup_obligations
 GROUP BY state
 ORDER BY state;
SELECT outcome, event_type, count(*)
  FROM astrasync_security_audit_events
 WHERE occurred_at >= now() - interval '1 hour'
 GROUP BY outcome, event_type
 ORDER BY outcome, event_type;
```

## Rollback

Rollback is gate-first and additive; do not drop tables or delete retained bindings.

1. Set `apiServer.connectionRollout.runtimeEnabled=false` first. This blocks new Starts containing
   `connection_ref` while leaving Catalog and Connection reads available.
2. Set `apiServer.connectionRollout.testsEnabled=false` to stop new test admission. Keep the
   executor running until queued/running tests reach zero, then it may be scaled down.
3. Set `apiServer.connectionRollout.mutationsEnabled=false` to stop Connection administration.
4. Keep `scheduler.connectionMaterialization.enabled=true` while any accepted Connection-backed
   epoch, materialization receipt, generated execution Secret, or pending cleanup obligation
   remains. Existing epochs may finish or be stopped normally; rotation or disablement never
   substitutes a different generation underneath them.
5. After every affected epoch is terminal and cleanup is confirmed, set
   `scheduler.connectionMaterialization.enabled=false` and roll back API/Scheduler/compiler/runtime
   images as one inventory-compatible set.
6. If the candidate inventory is faulty, reactivate the prior catalog-compatible image set. Do not
   relabel incompatible Connections or rewrite immutable generations.

Do not delete external provider Secrets during AstraSync rollback. Do not drop Connection
generations, Job/execution bindings, receipts, tombstones, idempotency rows, or audit events while a
retained binary, execution, retry, or retention policy can reference them.

Rollback is complete only when new gated operations are rejected as expected, existing Jobs remain
readable, all accepted epochs have converged, generated execution credential Secrets are gone, and
the pending cleanup count is zero.
