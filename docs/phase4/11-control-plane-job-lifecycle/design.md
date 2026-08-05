# Phase 4 Slice 11 Design

## Goals

1. Define one control-plane JobSpec and lifecycle vocabulary across protobuf, Go domain objects,
   PostgreSQL documents, and the Kubernetes SyncJob API.
2. Provide durable namespace-scoped Job CRUD with immutable UID and optimistic versions.
3. Make Start and Stop idempotent desired-state commands and fence execution updates by epoch.
4. Run a production-shaped API process with gRPC, JSON gateway, health/readiness, migrations, and
   graceful shutdown.
5. Establish an idempotent Kubernetes reconciler and Lease-elected controller manager.
6. Make generated protobuf and CRD artifacts reproducible and verified in CI.

## Non-goals

- Launching or supervising the Java Coordinator and Workers.
- Scheduler admission, resource placement, or execution heartbeat processing.
- Authentication, authorization, audit history, or a web console.
- Multi-region active/active control-plane operation.

## Canonical Model

`job.Spec` mirrors the Java JobSpec configuration: connector/options source and sink, transforms,
`delivery.guarantee`, and `runtime.maxBatchRecords`. `connectionRef` is optional control-plane
metadata for later secret/catalog resolution. Connector canonical names and delivery values are
validated at the domain boundary.

`job.Job` combines the namespace/name key, immutable UUID, optimistic version, spec, lifecycle
status, and creation/update timestamps. Status contains desired state, observed state, epoch,
restart count, start/end time, optional checkpoint summary, and optional failure. Domain validation
rejects internally inconsistent state before repository writes and after PostgreSQL reads.

## Persistence and Concurrency

PostgreSQL stores identity and concurrency columns separately while keeping spec and status as
JSONB. `(namespace, name)` is the primary key and UID is unique. Create maps uniqueness violations
to `AlreadyExists`. Update increments the database version only when namespace, name, UID, and
expected version match. Delete likewise requires the expected version. A failed write distinguishes
NotFound from Conflict for stable gRPC status mapping.

List ordering is lexical by name and pagination is namespace-scoped. Count and page retrieval keep
the total visible even when the requested page has no rows.

## Lifecycle and Idempotency

Start is accepted from Created, Canceled, Finished, and Failed. It increments epoch, increments
restart count after the first execution, clears terminal failure/end time, and moves to
Initializing with desired Running. Stop moves Initializing or Running to Canceling with desired
Stopped. Repeating an already-satisfied command returns the current value unchanged.

Optimistic updates also handle concurrent duplicate commands. If Update reports a version conflict,
the service rereads the Job; when the winning update already established the requested desired
state, the losing request succeeds with that current Job. Unrelated concurrent changes still return
Aborted.

`Advance` is the execution-observation boundary. It rejects stale epochs and illegal transitions,
requires failure details exactly for Failed, and records terminal end time. Slice 11 defines and
tests this boundary; later slices expose it to the dispatch/observation loop.

## API Process

The protobuf service exposes Create, Get, List, Update, Delete, Start, Stop, and GetStatus. Domain
validation maps to InvalidArgument or FailedPrecondition; missing rows, duplicates, and version
conflicts map to NotFound, AlreadyExists, and Aborted. The grpc-gateway is generated from the same
descriptor and registered against the gRPC listener. Readiness performs a bounded database ping.

## Kubernetes Projection

The SyncJob CRD is generated from `control-plane/controller/api/v1`, which imports the shared Job
types. A single API source eliminates the former deployment/operator type copy. The reconciler
defaults desired state to Stopped, allocates one epoch for each restartable-to-running transition,
and moves an active resource to Canceling when stopped. Status updates use the Kubernetes status
subresource. The controller manager provides Lease election and standard health probes.

This projection is a reconciliation foundation, not a second PostgreSQL repository. A later slice
will connect Kubernetes resources, scheduler admission, and execution observations to the durable
Job service.

## Generation and Delivery

Buf owns the protobuf module rooted at `api/protobuf`; source-relative generation produces exactly
`control-plane/api-server/gen/go/v1`. Controller-gen owns the SyncJob CRD. CI lints the descriptor,
regenerates both artifact sets, rejects drift, tests all Go modules, and runs the PostgreSQL
repository integration test against a real PostgreSQL service.
