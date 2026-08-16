# Rollback

## Purpose

Roll back a release, schema migration, or admin action that introduced
a regression. The procedure covers three triggers:

- A bad release that broke the API Server, Console, or data plane.
- A schema migration that added a column or constraint the
  application code did not handle.
- An accidental destructive admin action (for example, a
  `revoke-session` against the wrong principal, or a
  `disable-principal` against the platform administrator).

Every rollback preserves the epoch fencing invariant (ADR-006). The
procedure never resurrects a previous epoch; it either rolls the
deployment to a previous release or marks the affected rows as
historical without reviving the active state.

## Pre-conditions

- `<control-plane-version>` — the deployed AstraSync control plane
  release. The rollback target is one or two releases behind.
- `<rollback-target-version>` — the release the operator is rolling
  back to. The target must be a release the operator has tested
  against the current database schema.
- `<rollback-reason>` — a short string the operator records in the
  incident ticket (for example `bad-release`, `schema-regression`,
  `accidental-admin-action`).
- `<incident-ticket>` — the incident ticket ID the operator opens
  before running the rollback.
- The operator has a jump host with `kubectl`, the object storage
  CLI, and `astra-auth-admin` installed.

## Procedure

### Bad release

1. **Open the incident ticket.** The ticket records
   `<rollback-reason>`, the affected release, and the rollback
   target. The ticket is the durable record of the rollback decision.
2. **Stop the Scheduler and Controller.** The Scheduler and
   Controller are responsible for starting new Jobs and reconciling
   existing Jobs. Stopping them prevents the bad release from
   scheduling new work. Existing Jobs continue to run on the data
   plane workers.
   Expected output: `kubectl get pods` shows the Scheduler and
   Controller pods in `Terminating` state.
   Rollback: re-create the pods from the previous release's manifest.
3. **Roll the API Server and Console deployments.** Update the
   deployments to the `<rollback-target-version>` image and roll the
   pods. The production TLS and OIDC gates must remain closed; the
   target release must support the same environment variables the
   current release uses.
   Expected output: the API Server and Console pods restart with
   the target image.
   Rollback: re-deploy the bad release if the target image fails to
   start.
4. **Verify the API Server and Console.** Sign in to the Console with
   a non-affected principal and confirm the tenant list and the
   audit explorer render. The API Server logs must show the
   `<rollback-target-version>` value at startup.
   Expected output: a successful sign-in and a tenant list that
   matches the pre-rollback list.
   Rollback: stop. The verification is informational.
5. **Resume the Scheduler and Controller.** Re-create the Scheduler
   and Controller pods from the `<rollback-target-version>` manifest.
   The pods resume reconciling existing Jobs and may schedule new
   work against the previous release.
   Expected output: the Scheduler and Controller pods reach the
   `Ready` state.
   Rollback: stop. The Scheduler and Controller are restored.
6. **Close the incident ticket.** Record the rollback in the ticket
   and link the post-incident review.

### Schema regression

1. **Open the incident ticket.** Record the schema migration name,
   the affected columns or constraints, and the application code
   that did not handle the change.
2. **Stop the API Server.** The API Server is the only component
   that writes to the database in production. Stopping the API
   Server prevents the application code from issuing queries that
   conflict with the new schema.
3. **Roll the schema.** Run the down migration that reverses the
   schema change. The migration is recorded in the database migration
   table. The down migration is reversible because every up
   migration has a paired down migration; this is the Slice 18
   audit-table discipline applied to the wider schema.
4. **Roll the API Server and Console deployments.** Same as the bad
   release procedure.
5. **Verify.** Run the smoke queries against the rolled-back schema.
   The query plans must match the pre-migration plans.
6. **Resume.** Start the Scheduler and Controller as in the bad
   release procedure.

### Accidental destructive admin action

1. **Open the incident ticket.** Record the principal UUID, the
   admin action, the operator UUID, and the timestamp.
2. **Identify the recovery mechanism.** The recovery mechanism
   depends on the action:
   - `disable-principal` against a non-platform administrator:
     re-enable by re-running `bootstrap-tenant`.
   - `disable-principal` against the platform administrator:
     bootstrap a new platform administrator from a different IdP
     subject.
   - `revoke-session`: the sessions are deleted and cannot be
     restored. Inform the principal and have them sign in again.
   - `suspend-tenant`: re-activate with `reactivate-tenant`.
3. **Run the recovery command.** The recovery commands are
   idempotent; re-running them does not duplicate rows.
4. **Verify.** Run `show-tenant` or `show-revision` against the
   affected tenant. The recovery is successful when the principal or
   tenant returns to the ACTIVE state.
5. **Close the incident ticket.** Record the recovery command, the
   timestamp, and the operator UUID.

## Verification

- The API Server logs show the `<rollback-target-version>` value at
  startup.
- The Console sign-in succeeds with a non-affected principal.
- The Scheduler and Controller pods reach the `Ready` state.
- The audit table contains a `rollback` event with the
  `<rollback-target-version>`, the operator UUID, and the incident
  ticket ID.
- The deployment-side artifact registry contains the rollback
  manifest.

## Rollback

- For a bad release: the rollback target itself may be bad. The
  operator records the failure in the incident ticket and rolls
  forward to a different release.
- For a schema regression: the down migration may fail. The operator
  restores the database from the latest backup using the backup
  runbook, then re-runs the down migration.
- For an accidental admin action: the recovery commands are
  idempotent; re-running them does not duplicate rows. If the
  recovery fails, the operator opens a new incident ticket and
  escalates to the platform team.

## Security boundary

- The rollback never resurrects a previous epoch. ADR-006 forbids
  resurrecting an epoch that was fenced out by the Controller; the
  rollback procedure stops at the Scheduler and Controller so the
  epoch fencing invariant is preserved.
- The rollback manifest is recorded in the deployment-side artifact
  registry. The manifest is the durable record of the rollback; the
  AstraSync repository does not store rollback manifests.
- The recovery commands never delete an audit event. The audit table
  is append-only; the rollback's audit events are recorded
  alongside the action that triggered the rollback.

<!-- placeholders: control-plane-version, rollback-target-version, rollback-reason, incident-ticket, principal-id, operator-id, tenant-uuid -->