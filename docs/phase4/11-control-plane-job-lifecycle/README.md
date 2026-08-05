# Durable Control-plane Job Lifecycle

Slice 11 is the operational entry point for the Phase 4 control plane. It exposes the versioned
Job API over gRPC on port `50051` and grpc-gateway JSON on port `8080`, backed by PostgreSQL.

## Configuration

The API server requires `DATABASE_URL`. It accepts optional `GRPC_LISTEN_ADDRESS`,
`GRPC_GATEWAY_ENDPOINT`, and `HTTP_LISTEN_ADDRESS`. Startup connects to PostgreSQL and applies the
embedded idempotent migration before serving traffic. `GET /health` reports process health and
`GET /ready` checks the database.

The generated JSON gateway currently exposes one POST route per RPC:

```text
/astra.control.v1.JobService/CreateJob
/astra.control.v1.JobService/GetJob
/astra.control.v1.JobService/ListJobs
/astra.control.v1.JobService/UpdateJob
/astra.control.v1.JobService/DeleteJob
/astra.control.v1.JobService/StartJob
/astra.control.v1.JobService/StopJob
/astra.control.v1.JobService/GetJobStatus
```

Create accepts the same nested JobSpec shape as the Java runtime:

```json
{
  "namespace": "default",
  "name": "orders-cdc",
  "spec": {
    "source": {
      "connector": "mysql-cdc",
      "options": {"database": "shop", "tables": "shop.orders"}
    },
    "sink": {
      "connector": "jdbc",
      "options": {"table": "orders", "keyColumns": "id"}
    },
    "delivery": {"guarantee": "DELIVERY_GUARANTEE_EXACTLY_ONCE"},
    "runtime": {"maxBatchRecords": 2048}
  }
}
```

Update and Delete require a positive `expectedVersion`. Start and Stop accept zero to omit the
precondition or a positive expected version. Retrying an already-satisfied Start or Stop returns
the current Job without incrementing its version or epoch, including when a concurrent matching
command won the optimistic update race.

## Kubernetes Representation

The generated SyncJob CRD uses the same source, sink, transforms, delivery, runtime, desired state,
observed state, epoch, checkpoint, and failure fields. `spec.state` defaults to `STOPPED`; setting it
to `RUNNING` moves a restartable resource to `INITIALIZING` exactly once and allocates a new epoch.
The controller exposes `/healthz` and `/readyz` and uses a Kubernetes Lease for leader election.

## Lifecycle Rules

```text
CREATED --Start--> INITIALIZING --> RUNNING --> FINISHED
                              |          |
                              +----------+--> FAILED
INITIALIZING/RUNNING --Stop--> CANCELING --> CANCELED
CANCELED/FINISHED/FAILED --Start--> INITIALIZING (new epoch)
```

Specs cannot be changed and Jobs cannot be deleted while `INITIALIZING`, `RUNNING`, or
`CANCELING`. Execution updates must carry the current epoch. Terminal transitions set desired state
to `STOPPED` and record `endTime`.

See [design.md](design.md), [implementation-plan.md](implementation-plan.md), and
[verification.md](verification.md).
