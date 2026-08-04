# Phase 1 Slice 05: Distributed JDBC Operational Slice

Slice 05 connects the Phase 1 components into a deployable JDBC full-load path and completes Phase 1.
One Coordinator enumerates a static JDBC range plan, sends descriptors to stable TCP Workers, and
persists each successful split before the run can be resumed.

## Delivered

- Production `JdbcWorkerTaskFactoryProvider` in the standard Worker shaded JAR and image.
- Executable shaded Coordinator JAR and image.
- Validated Coordinator environment configuration and `worker-id@host:port` endpoints.
- Descriptor-only remote tasks that cannot open connector resources on the Coordinator.
- Shared immutable JobSpec loading, with JDBC connections and task resources created on Workers.
- Real TCP integration coverage for two-Worker full load, durable partial success, restart, complete
  rerun, and split-plan drift rejection.
- Two-Worker Docker Compose demo backed by PostgreSQL and a persistent progress volume.
- Helm Worker StatefulSet, headless Service, one-shot Coordinator Job, shared Secret, and required
  progress PVC.

## Docker Demo

From the repository root, build and start PostgreSQL and both Workers, then invoke the one-shot
Coordinator:

```bash
docker compose -f deployment/docker/docker-compose.dev.yml up --build --wait postgres worker-0 worker-1
docker compose -f deployment/docker/docker-compose.dev.yml run --rm --build coordinator
docker compose -f deployment/docker/docker-compose.dev.yml exec postgres \
  psql -U astra -d astrasync -c "TABLE target_data;"
```

The first Coordinator run reports two executed splits. Running it again with the same named progress
volume reports two resumed splits and zero executed splits. `docker compose down` retains both named
volumes; `docker compose down -v` removes the demo database and progress state.

The demo JobSpec is
[`deployment/docker/examples/jdbc-job.yaml`](../../../deployment/docker/examples/jdbc-job.yaml).
Production credentials belong in a secret-managed JobSpec, not in source control.

## Helm Deployment

Create an immutable Secret whose selected key contains the complete JobSpec, and create a PVC that
supports file locks and atomic rename. The JobSpec metadata name is the durable job ID and must be a
lowercase DNS label.

```yaml
apiVersion: v1
kind: Secret
metadata:
  name: astrasync-jdbc-job
immutable: true
stringData:
  job.yaml: |
    # Complete sync.astrasync.io/v1 JobSpec goes here.
---
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: astrasync-jdbc-progress
spec:
  accessModes: [ReadWriteOnce]
  resources:
    requests:
      storage: 1Gi
```

Install the data-plane-only example after setting Worker and Coordinator image repositories and tags
as needed:

```bash
helm upgrade --install astrasync deployment/helm/astrasync \
  -f deployment/helm/astrasync/examples/jdbc-values.yaml
```

The chart generates endpoints such as
`astrasync-worker-0@astrasync-worker-0.astrasync-worker:50051`. A Helm release revision creates a new
Coordinator Job, while the stable StatefulSet identities and existing PVC allow the new invocation to
resume. Only one Coordinator Job may actively execute a given JobSpec name.

## Delivery Boundary

The sink commits each JDBC batch before the Worker reports task success, and the Coordinator records
the successful split afterward. Process or network loss inside that interval can replay the complete
split. There is no intra-split checkpoint, shared sink transaction, automatic retry loop, epoch
fencing, or exactly-once claim in Phase 1.

See [design.md](design.md), [verification.md](verification.md), and
[ADR-025](../../adr/adr-025-distributed-jdbc-operational-slice.md).
