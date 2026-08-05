# ADR-029: Durable Desired-state Job Lifecycle

## Status

Accepted

## Context

The completed data plane can parse and execute versioned JobSpecs, but the Go control plane had no
durable Job record or authoritative lifecycle. API, Kubernetes, and data-plane representations had
diverged, and process-local commands could not provide optimistic concurrency, retry semantics, or
execution fencing across replicas.

Phase 4 needs a durable boundary before scheduler and Coordinator integration. Clients must be able
to retry Start and Stop safely, controllers must converge desired state without allocating duplicate
executions, and stale execution reports must not mutate a newer run.

## Decision

Define a shared Go Job domain model compatible with the Java JobSpec and use it from the protobuf
service, PostgreSQL repository, and Kubernetes SyncJob API. A Job has a namespace/name key,
immutable UUID, optimistic version, desired state, observed state, and monotonically increasing
execution epoch.

Use PostgreSQL as the authoritative store for Job API records. Persist identity/version columns and
JSONB spec/status documents. Every mutation uses compare-and-swap version checks. Start and Stop
are desired-state commands: repeated matching commands do not change version or epoch, and a loser
of a concurrent matching update rereads and returns the winning state. Observed transitions require
the current epoch and follow an explicit state machine.

Generate the gRPC, JSON gateway, and SyncJob CRD artifacts from versioned source contracts. Run the
Kubernetes controller under Lease election and make its desired-state projection idempotent. Keep
actual Coordinator dispatch and execution observation outside this slice; they will consume the
same epoch-fenced boundary.

## Consequences

- Job CRUD and lifecycle survive API process restarts and support multiple API replicas.
- Retries are safe without request-id storage because Start and Stop converge on desired state.
- Optimistic versions expose conflicting edits instead of silently overwriting them.
- Epochs provide the fencing token needed by later scheduler and Coordinator integrations.
- PostgreSQL and Kubernetes currently represent two ingress/projection paths; later reconciliation
  must define which adapter synchronizes them rather than introducing another Job model.
- Schema evolution of JSONB documents and protobuf fields now requires explicit compatibility and
  migration review.
