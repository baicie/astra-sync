# Controller Convergence and HA

Phase 4 Slice 13 connects the Kubernetes `SyncJob` API to the PostgreSQL lifecycle authority and
adds liveness evidence to Scheduler dispatch. Controller replicas use Kubernetes leader election,
while PostgreSQL version checks and execution epochs fence concurrent writers.

## Runtime Flow

```text
SyncJob spec.state/spec -> Controller -> PostgreSQL Job desired state/spec
                                      ^                 |
                                      |                 v
                              status projection <- Scheduler dispatch
                                                        |
 Coordinator -> authenticated heartbeat -> Scheduler Service
                                                        |
                                     timeout CAS -> STOPPING/failure
                                                        |
                                   labeled orphan resource sweep
```

The Controller creates the PostgreSQL row when a CRD first appears, imports changes only while an
execution is inactive, applies desired start/stop transitions, and mirrors the PostgreSQL status
back to the CRD. A deletion finalizer stops an active execution before deleting the durable row.

Each dispatch row owns a random UUID bearer token. The token is mounted from the immutable JobSpec
Secret, and the Coordinator sends an initial authenticated heartbeat before execution followed by
periodic reports. Kubernetes Job activity never refreshes liveness. A Scheduler replica may take
over an expired lease or stale heartbeat, but it always reuses the same `(job UID, execution epoch)`
and deterministic Kubernetes resource names. Timeout fencing is an atomic PostgreSQL transition to
`STOPPING`, so a heartbeat that arrives before the fence wins and a replacement Scheduler can
resume failure cleanup after the fencing owner exits.

The orphan sweep deletes only resources carrying the Scheduler managed-by label plus a valid AstraSync
UID/epoch identity. Active identities retain all resources, terminal identities retain only their
Coordinator Job for post-mortem TTL, and identities absent from dispatch history are fully removed.

## Configuration

Controller requires `DATABASE_URL`. Its `--status-refresh-interval` controls periodic status
projection and defaults to `5s`.

Scheduler adds `SCHEDULER_HEARTBEAT_TIMEOUT`, defaulting to `2m`, and
`SCHEDULER_HEARTBEAT_INTERVAL_MS`, defaulting to `10000`. The timeout must exceed two heartbeat
intervals and the Scheduler reconciliation interval. Helm exposes the Scheduler heartbeat endpoint
through an internal ClusterIP Service; the generated execution URL and token are injected into only
the Coordinator for that UID/epoch.

## Records

- [Design](design.md)
- [Implementation plan](implementation-plan.md)
- [HA operations and failover drills](operations.md)
- [Verification](verification.md)
- [ADR-031: PostgreSQL lifecycle convergence and execution liveness](../../adr/adr-031-controller-convergence-and-ha.md)
