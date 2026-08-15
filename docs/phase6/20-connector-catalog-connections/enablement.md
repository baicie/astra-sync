# Phase 6 Slice 20 Operator Enablement

## Purpose

This document explains how an operator enables Slice 20 — the Connector Catalog and tenant
Connection references — in a deployment. Slice 20 is implemented behind four feature
gates; the gates are independent so an operator can adopt them in order. This doc
captures the gate keys, the prerequisites, the staged rollout steps, the production
enablement steps, and the rollback procedure.

The authoritative state is recorded in the [Slice 20 design](design.md), the
[Slice 20 implementation plan](implementation-plan.md), the [Slice 20
verification](verification.md), the [Descriptor contract](descriptor-contract.md),
the [Connection lifecycle](connection-lifecycle.md), the [Security and
materialization](security-and-materialization.md), the [Authorization
matrix](authorization-matrix.md), and the [Migration and rollback
runbook](migration-and-rollback.md).

## Rollout Gates

| Helm value | Environment variable | Default | Effect when disabled |
|---|---|---|---|
| `apiServer.connectionRollout.mutationsEnabled` | `CONNECTION_MUTATIONS_ENABLED` | `false` | Connection Create / Update / Rotate / Enable / Disable / Delete is rejected; reads remain available |
| `apiServer.connectionRollout.testsEnabled` | `CONNECTION_TESTS_ENABLED` | `false` | New `TestConnection` calls are rejected; admitted tests may drain |
| `apiServer.connectionRollout.runtimeEnabled` | `CONNECTION_RUNTIME_ENABLED` | `false` | Start of a Job containing `connection_ref` is rejected |
| `scheduler.connectionMaterialization.enabled` | `SCHEDULER_CONNECTION_MATERIALIZATION_ENABLED` | `false` | Scheduler does not resolve or create execution credential Secrets |
| `connectionTestExecutor.enabled` | n/a | `false` | The isolated test queue has no consumer |

Helm refuses to enable runtime admission without Scheduler materialization, and refuses
test admission without the executor. Catalog and Connection read paths are not disabled
by rollback.

## Prerequisites

Before any gate is enabled, the operator confirms:

1. Slice 18 OIDC validation, tenant RBAC, transactional audit, and IdentityService /
   AccessService administration are deployed and verified per the
   [Slice 18 verification](../18-auth-rbac-audit/verification.md).
2. Slice 19 Console mutation workflows are deployed and verified per the
   [Slice 19 enablement](../19-job-operations/enablement.md). `CONSOLE_JOB_MUTATIONS_ENABLED`
   must be `true` for any tenant that needs to exercise Connection-backed Jobs.
3. The deployment connector inventory is captured by the Java compiler profile and the
   resulting `ServiceLoader` set is reproducible. The operator runs `make catalog-check`
   against the candidate compiler, API, Coordinator, and Worker images and records the
   matching inventory revisions.
4. PostgreSQL backups cover the rollout window and the additive migrations
   (`astrasync_connections`, `astrasync_connection_generations`,
   `astrasync_job_connection_bindings`, `astrasync_execution_connection_bindings`,
   `astrasync_connection_materialization_receipts`, and the corresponding audit /
   tombstone tables) are confirmed to apply cleanly.
5. The Kubernetes Secret provider namespace, server-owned tenant namespaces, and the
   per-tenant `astrasync/tenant` label are provisioned through the deployment's standard
   tenant bootstrap. The bootstrap does not move credential bytes out of the existing
   provider; operators use the approved secret provisioning workflow to create immutable
   Secrets.
6. The Controller and Scheduler service actors and their database roles are configured
   per the [Security and materialization](security-and-materialization.md) doc. The
   Scheduler materialization role has `create` / `get` on its own tenant namespaces and
   no broad cluster read authority.

If any prerequisite is missing, the control plane refuses to enable the corresponding
gate and emits a structured startup error.

## Catalog Enablement

Catalog reads (`ListConnectorDescriptors`, `GetConnectorDescriptor`) do not require a
gate. They become available as soon as the inventory activation row is committed and
the API startup validates the descriptor revisions. The rollout step is:

1. Deploy the additive schema and the Java compiler profile with the candidate
   connectors.
2. Run `make catalog-check` to confirm the API / Coordinator / Worker images match the
   compiler inventory.
3. Confirm the catalog activation row:

   ```sql
   SELECT execution_profile, inventory_revision, activated_at
     FROM astrasync_connector_inventory_activation
    ORDER BY execution_profile;
   ```

4. Authenticate as `platform_admin` and call `ListConnectorDescriptors` from a CLI or
   the BFF. Verify the projection excludes descriptor internals, class paths, and
   service addresses.

The Catalog is read-only and is enabled by default once Slice 18 is in place.

## Staged Connection Enablement

### Stage A — Connection administration (`CONNECTION_MUTATIONS_ENABLED`)

1. Confirm the Slice 19 staging tenant also has the
   `connections.manage` / `connections.use` permission mapped per the
   [Authorization matrix](authorization-matrix.md).
2. Enable `CONNECTION_MUTATIONS_ENABLED=true` for one staging tenant.
3. Exercise Create, Read, List, Update, Rotate, Enable, Disable, and Delete in the
   staging tenant using the [Connection lifecycle](connection-lifecycle.md) as the
   expected sequence. Reconcile the audit, idempotency, and tombstone counts against the
   staging snapshot.
4. Confirm that no provider call happens inside a PostgreSQL mutation transaction. The
   test suite covers this, but the operator also confirms the API logs do not show
   provider calls in the same trace as the transaction commit.
5. Block any staging tenant whose JobSpecs still reference raw sensitive options. The
   block is enforced by the next Slice 19 mutation; the operator records the block in
   the rollout memo.

### Stage B — Connection tests (`CONNECTION_TESTS_ENABLED`)

1. Enable the isolated connection test executor (`connectionTestExecutor.enabled=true`)
   on its own identity, namespace, queue, network policy, and resource quota. The
   executor never shares a Kubernetes service account with the data plane.
2. Enable `CONNECTION_TESTS_ENABLED=true` for the same staging tenant.
3. Exercise `TestConnection` and `GetConnectionTest` for the four supported
   connectors (CSV, JDBC, MySQL CDC, PostgreSQL CDC). Verify the executor never
   accepts a caller SQL, arbitrary redirect, or vendor text response. Verify the
   result codes never include credential bytes.
4. Verify the executor honors the bounded admission limit, the request deadline, and
   the queue FencingClaim semantics. Confirm the lease cannot be reclaimed by a
   different replica after expiry.
5. Reconcile test receipts against the database queue; the queue must drain without
   leftovers.

### Stage C — Connection runtime (`CONNECTION_RUNTIME_ENABLED`)

1. Enable `SCHEDULER_CONNECTION_MATERIALIZATION_ENABLED=true` together with
   `CONNECTION_RUNTIME_ENABLED=true`. Helm refuses to enable runtime admission without
   materialization.
2. Exercise the full Slice 19 lifecycle with `connection_ref`:
   - Create the Connection and bind it to a Job.
   - Start the Job and confirm the Scheduler captures immutable source / sink
     generations.
   - Rotate the Connection and confirm subsequent Start fails closed on the
     existing execution.
   - Disable / re-enable the Connection and confirm Start honors the disabled state.
   - Delete the Connection and confirm the Job binding remains stable.
   - Force a credential Secret recreation (without the approved workflow) and
     confirm Start is rejected with the exact UID mismatch error.
3. Verify the Scheduler materializer reconciles only validated AstraSync-owned orphan
   Secrets and never touches external provider Secrets.
4. Verify the runtime mounts the credential envelope read-only and the
   `RuntimeCredentialLoader` consumes exactly the documented fields.

## Production Enablement

1. Complete the staged acceptance memos for Stages A, B, and C. Each memo records the
   staging tenant, the verification date, the operators who signed off, and the
   reconciliation results.
2. Schedule the production enablement with the change review board. The review cites
   this doc, the staging memos, and the [Slice 20 verification](verification.md).
3. Enable the gates on one production tenant first. The tenant must already be reachable
   through Slice 18 OIDC and have at least one `tenant_admin`.
4. Observe the acceptance signals per the [Migration and rollback
   runbook](migration-and-rollback.md):
   - mutation latency, no-op rate, and conflict rate,
   - test admission and timeout rate,
   - materialization latency and lease loss rate,
   - audit failure rate (must remain at zero; any failure triggers rollback),
   - receipt reconciliation drift (must remain at zero).
5. Expand to additional tenants one at a time. Each tenant has the same acceptance
   window.

## Rollback Procedure

Rollback is performed per gate. Each rollback step is independent and preserves the
already-admitted epochs.

| Gate | Rollback action | Effect |
|---|---|---|
| `CONNECTION_MUTATIONS_ENABLED` | Set to `false` | New mutations rejected; accepted epoch remains intact until it finishes or stops |
| `CONNECTION_TESTS_ENABLED` | Set to `false` | New tests rejected; admitted tests drain |
| `CONNECTION_RUNTIME_ENABLED` | Set to `false` | Start of Jobs containing `connection_ref` is rejected; accepted executions continue |
| `SCHEDULER_CONNECTION_MATERIALIZATION_ENABLED` | Set to `false` | Materializer stops creating new credential Secrets; cleanup continues until the last execution finishes |
| `connectionTestExecutor.enabled` | Set to `false` | Test queue has no consumer; leases expire and tests fail closed |

Helm refuses to enable runtime admission without materialization and refuses test
admission without the executor, so a partial rollback cannot leave the system in an
inconsistent state.

## Observability

Operators monitor Slice 20 enablement using the same dashboards as Slice 18 and 19.
Critical signals:

| Signal | Source | Threshold |
|---|---|---|
| `connection.mutation.denied` counter | audit log filtered by `connection.create` / `connection.update` / `connection.rotate` / `connection.enable` / `connection.disable` / `connection.delete` and outcome `DENIED` | spike indicates permission misconfiguration |
| `connection.mutation.audit_failed` counter | audit log filtered by the same event types and outcome `AUDIT_WRITE_FAILED` | must remain at zero |
| `connection.test.timeout` counter | test receipts with status `TIMEOUT` | spike indicates executor admission or DNS pinning issue |
| `connection.materialization.lease_loss` counter | audit log filtered by `materialization.lease.lost` | any non-zero value triggers investigation |
| `connection.materialization.audit_failed` counter | audit log filtered by `materialization.audit.failed` | must remain at zero |
| `connection.start.rejected` counter | audit log filtered by `job.start` with reason `CONNECTION_RUNTIME_DISABLED` | spike indicates a tenant attempted Start before the gate was enabled |

The acceptance memo records the baseline values for each tenant. Any deviation during
production enablement triggers the rollback procedure.

## Records

- [Slice 20 README](README.md)
- [Slice 20 Design](design.md)
- [Slice 20 Descriptor Contract](descriptor-contract.md)
- [Slice 20 Connection Lifecycle](connection-lifecycle.md)
- [Slice 20 Security and Materialization](security-and-materialization.md)
- [Slice 20 Authorization Matrix](authorization-matrix.md)
- [Slice 20 Implementation Plan](implementation-plan.md)
- [Slice 20 Migration and Rollback Runbook](migration-and-rollback.md)
- [Slice 20 Verification](verification.md)
- [Slice 18 admin runbook](../18-auth-rbac-audit/admin-runbook.md)
- [Slice 19 operator enablement](../19-job-operations/enablement.md)
- [ADR-040: Deployment-authoritative Connector Descriptor Catalog](../../adr/adr-040-deployment-authoritative-connector-catalog.md)
- [ADR-041: External Secret References and Epoch-scoped Credential Materialization](../../adr/adr-041-external-secrets-epoch-credential-materialization.md)
