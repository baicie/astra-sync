# Backup

## Purpose

Back up the three storage backends that AstraSync depends on, and
restore from those backups without violating the epoch fencing
invariant (ADR-006) or the exactly-once capability negotiation
invariant (ADR-009). The three backends are:

- PostgreSQL — coordination metadata (jobs, tenants, audit, RBAC).
- etcd — runtime leases (Controller / Scheduler leader election,
  active epoch).
- Object storage — large state and spillable exchange (data plane
  RocksDB and Arrow spill).

The procedure covers the daily / weekly snapshot cadence, the
restore drill the operator runs to validate the cadence, and the
region-failover input that Slice 25 will inherit.

## Pre-conditions

- `<control-plane-version>` — the deployed AstraSync control plane
  release. The backup tool versions and the metadata format differ
  by version; the operator records the version in the backup
  manifest.
- `<postgres-host>` and `<postgres-database>` — the PostgreSQL
  primary endpoint and the database name.
- `<etcd-cluster-endpoints>` — the etcd cluster client endpoints.
  The procedure uses `etcdctl snapshot save`.
- `<object-storage-bucket>` — the bucket where the data plane writes
  RocksDB checkpoints and Arrow spill. The bucket name follows the
  naming convention recorded in the Helm chart values.
- `<backup-bucket-url>` — the bucket where the operator stores the
  PostgreSQL / etcd / state-backend backups. The bucket must have a
  cross-region replication policy the operator's compliance team
  owns.
- `<encryption-key-id>` — the KMS key the operator uses to encrypt
  the backups at rest. The key must be the same key the operator
  uses for the live database encryption.
- The operator has a jump host with `pg_dump`, `etcdctl`, the object
  storage CLI, and `kubectl` installed.

## Procedure

### Daily snapshot

1. **Snapshot PostgreSQL.** Run `pg_dump --format=custom --file=
   <backup-bucket-url>/postgres/<snapshot-id>.dump <postgres-host>/<postgres-database>`.
   Use the database role the API Server uses; the role has the
   privileges required for `pg_dump` without `--no-owner`.
   Expected output: a dump file in the backup bucket whose size is
   within 10% of the previous day's dump.
   Rollback: re-run `pg_dump` if the file is corrupt or missing.
2. **Snapshot etcd.** Run `etcdctl snapshot save
   <backup-bucket-url>/etcd/<snapshot-id>.db` from one of the etcd
   cluster members. The snapshot is consistent across the cluster
   because `etcdctl` uses the leader's `raft` log.
   Expected output: a snapshot file in the backup bucket whose size
   is within 20% of the previous day's snapshot.
   Rollback: re-run `etcdctl snapshot save` from a different member
   if the snapshot is corrupt.
3. **Snapshot object storage.** Use the object storage CLI to copy
   the live data plane bucket to
   `<backup-bucket-url>/object-storage/<snapshot-id>/`. The copy is
   incremental; the operator records the source and destination in
   the backup manifest.
   Expected output: a manifest file listing every object copied and
   the byte count.
   Rollback: re-run the copy if the manifest shows missing objects.

### Restore drill (weekly)

1. **Restore PostgreSQL.** Spin up a scratch PostgreSQL instance,
   restore the latest snapshot, and run a set of smoke queries
   against the restored database. The smoke queries must include
   `SELECT count(*) FROM jobs;` and `SELECT count(*) FROM
   audit_events;`. The counts must be within 1% of the live counts.
   Expected output: a scratch database with row counts matching the
   live database.
   Rollback: stop. The drill is read-only against the live database.
2. **Restore etcd.** Restore the latest etcd snapshot into a
   scratch cluster and run `etcdctl endpoint status` to confirm the
   leader election works. The scratch cluster must elect a leader
   within 10 seconds.
   Expected output: a healthy scratch etcd cluster.
3. **Restore object storage.** Compare the object count and the
   total byte count between the live bucket and the backup bucket.
   The counts must match within 1%.
   Expected output: a comparison report with matching counts.

### Region failover input (Slice 25)

1. **Generate the failover bundle.** Combine the latest PostgreSQL,
   etcd, and object storage snapshots into a single bundle the
   failover runbook consumes. The bundle is a tar archive whose
   top-level directory names match the three backends.
2. **Verify the bundle.** Restore the bundle into the scratch
   environment and run the same smoke queries as the weekly drill.
   The bundle must restore cleanly; otherwise the operator records
   the failure and re-runs the daily snapshots.

## Verification

- The backup bucket contains one snapshot per backend per day. The
  manifest is dated and the manifest's snapshot IDs match the
  bucket's contents.
- The restore drill's smoke queries return row counts within 1% of
  the live database's row counts.
- The etcd scratch cluster elects a leader within 10 seconds.
- The object storage comparison report shows matching counts.
- The KMS audit log shows one encryption call per snapshot per day.

## Rollback

- For a daily snapshot: re-run the snapshot step. The previous
  snapshot is not invalidated by a failed snapshot.
- For a restore drill: the drill is read-only against the live
  database. The drill's scratch resources are torn down at the end
  of the procedure.
- For a region failover input: re-run the bundle generation. The
  bundle is a copy of the live snapshots; the live snapshots are
  unchanged.

## Security boundary

- The backup bucket has cross-region replication and an object lock
  policy. A misconfigured bucket exposes the snapshots to deletion
  and to geographic loss.
- The PostgreSQL role the procedure uses has read access to every
  schema but does not have DDL authority. The role is recorded in
  the deployment manifest and matches the role the API Server uses.
- The KMS key is the same key the live database uses. A different
  key prevents the restore from succeeding.
- The restore drill runs in a scratch environment isolated from the
  live database. The scratch environment's network policy blocks
  egress to the live database.

<!-- placeholders: control-plane-version, postgres-host, postgres-database, etcd-cluster-endpoints, object-storage-bucket, backup-bucket-url, encryption-key-id, snapshot-id -->