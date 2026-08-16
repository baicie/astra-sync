# Audit Retention

## Purpose

Manage the lifetime of the append-only audit table. The procedure
covers three triggers:

- Monthly partition rollover. The audit table is partitioned by
  `created_at` month; the operator must detach and archive the
  previous month.
- Partition detach. The operator decides to detach a partition before
  the monthly rollover because the partition is full or because a
  legal hold requires a separate archive.
- Retention window change. The operator extends or shortens the
  retention window in response to a compliance requirement (for
  example, GDPR right-to-erasure, or a new internal policy).

The procedure never deletes an audit row in place. Every retention
action either detaches a partition for archive or extends the
retention window. The append-only invariant from Slice 18 stays
intact.

## Pre-conditions

- `<audit-database-url>` — the PostgreSQL connection URL for the
  audit database. The URL must point at the deployment role used by
  the API Server, not the wider cluster administrator role.
- `<partition-schema>` — the schema that holds the partitioned audit
  table. The schema is `audit` by default; deployments that adopted
  the Slice 18 audit schema use this name.
- `<partition-table>` — the partitioned table name. The table is
  `audit_events` by default.
- `<archive-bucket-url>` — the object storage bucket where the
  operator stores detached partitions. The bucket must have a
  retention policy the operator's compliance team owns.
- `<retention-window-days>` — the operator's desired retention window
  in days. The default is 365 days. The operator must record the new
  value in the deployment-side policy register before running the
  retention change.
- The operator has a jump host with `psql` and the object storage
  CLI installed.

## Procedure

### Monthly partition rollover

1. **Identify the partition to detach.** Query
   `pg_inherits` for the partitions of `<partition-table>` whose
   `created_at` upper bound is older than the current month.
   Expected output: a partition name (for example
   `audit_events_2026_07`).
   Rollback: stop. The query is informational.
2. **Detach the partition.** Run `ALTER TABLE
   <partition-table> DETACH PARTITION <partition-name>;`. The detach
   is concurrent and does not block writes to the live partition.
   Expected output: a row in `pg_inherits` showing the partition is
   no longer attached.
   Rollback: `ALTER TABLE <partition-table> ATTACH PARTITION
   <partition-name>;`. The audit queries that depend on the
   partition resume working.
3. **Export the detached partition.** Use `pg_dump` or the object
   storage CLI to export the detached partition to
   `<archive-bucket-url>/<partition-name>.dump`. The export must
   preserve the schema, the table options, and the indexes.
   Expected output: a dump file in the archive bucket whose size
   matches the partition's `pg_relation_size`.
   Rollback: stop. The export is a copy; the partition is unchanged.
4. **Verify the export.** Restore the dump to a scratch database
   and run the audit explorer queries against the restored
   partition. The row counts and the principal IDs must match the
   live partition.
   Expected output: matching row counts and matching
   `subject_principal_id` distributions.
   Rollback: re-export if the dump is corrupt.

### Partition detach (out-of-cycle)

1. **Identify the partition.** Same query as the monthly rollover,
   but the operator may target any partition, not only the previous
   month.
2. **Apply a legal hold (if required).** If the operator needs to
   preserve the partition for a legal hold, set the partition's
   `pg_class.reloptions` to `read_only = true` before detaching.
   The hold prevents the operator from accidentally re-attaching the
   partition after the hold closes.
3. **Detach, export, and verify.** Same as the monthly rollover.

### Retention window change

1. **Record the new value.** Update the deployment-side policy
   register with the new `<retention-window-days>` value and the
   policy team's approval reference.
2. **Update the API Server configuration.** The audit retention
   window is read by the API Server at startup. The operator updates
   the deployment with the new value and rolls the API Server.
   Expected output: the API Server logs the new retention window
   value at startup.
   Rollback: revert the deployment.
3. **Validate the change.** Confirm that partitions older than the
   new retention window are still attached and queryable. The
   operator does not detach them automatically; the operator reviews
   each partition against the legal hold list before detaching.

## Verification

- `SELECT count(*) FROM <partition-schema>.<partition-table>;` returns
  a row count consistent with the operator's expected active
  retention.
- `SELECT relname FROM pg_inherits WHERE inhparent =
  '<partition-schema>.<partition-table>'::regclass;` lists the
  attached partitions. The list shrinks by one after each detach.
- The archive bucket contains one dump file per detached partition.
  The dump file sizes match `pg_relation_size` for the corresponding
  partition.
- The deployment-side policy register contains the
  `<retention-window-days>` value and the approval reference.

## Rollback

- For a partition detach: re-attach the partition. The audit
  queries that depend on the partition resume working. The export to
  the archive bucket stays; the operator must decide whether to
  delete the export.
- For a partition out-of-cycle detach under a legal hold: close the
  hold, then re-attach the partition. The export stays.
- For a retention window change: revert the deployment. The new
  value is not durable in AstraSync; the API Server reads it on every
  startup.

## Security boundary

- The audit table is append-only. The procedure never runs `DELETE`
  against the live table or against a detached partition. Detached
  partitions are archived; live partitions keep every row.
- The audit database role used by the procedure must not have DDL
  authority on tables outside the audit schema. The operator follows
  the Slice 18 audit role grant recorded in the deployment
  manifest.
- The archive bucket must have an object lock policy the operator's
  compliance team owns. A misconfigured bucket exposes the
  archived audit data to deletion.
- The retention window is operator-defined per environment. The
  procedure never assumes a specific value; the operator records the
  value before running the change.

<!-- placeholders: audit-database-url, partition-schema, partition-table, archive-bucket-url, retention-window-days, partition-name -->