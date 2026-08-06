# ADR-031: PostgreSQL Lifecycle Convergence and Execution Liveness

## Status

Accepted

## Context

Slice 12 dispatches a PostgreSQL Job into deterministic Kubernetes resources, but the Controller
still mutates a CRD status independently of PostgreSQL. Scheduler lease renewal also proves only
that a process is running; it does not prove that the execution is observable. A lost owner can
leave auxiliary resources behind after a partial Kubernetes failure.

## Decision

Use PostgreSQL as the lifecycle authority. The Controller imports CRD desired state and JobSpec with
optimistic version checks, projects durable status through the CRD status subresource, and uses a
deletion finalizer to stop and delete the durable Job before removing the CRD.

Store execution heartbeat timestamps separately from dispatch ownership leases. PostgreSQL creates
an execution-scoped UUID bearer token, Kubernetes mounts it from the immutable JobSpec Secret, and
the Coordinator sends an initial report followed by periodic authenticated reports. Kubernetes Job
activity is observation only and never refreshes liveness.

A Scheduler replica may take over an expired lease or stale heartbeat under the existing PostgreSQL
advisory lock and UID/epoch fencing. Before failing an execution, it atomically verifies owner, lease,
phase, and heartbeat age while changing the dispatch to `STOPPING`. This lets a concurrent heartbeat
win before the fence and lets a replacement replica resume cleanup after the fencing replica exits.
The durable Job failure reason is `HeartbeatTimeout`.

Run a periodic orphan sweep only for resources with the Scheduler managed-by label and valid
UID/epoch labels. Keep auxiliary resources for active dispatches, remove them for terminal or absent
dispatches, and keep Coordinator Jobs for known terminal dispatches until post-mortem TTL.

## Consequences

The control plane has one durable state authority and can survive Controller/Scheduler process
failover without changing execution identity. Status projection is eventually consistent and
requires a refresh interval. A prolonged Kubernetes outage can produce failed executions and
requires the sweep to finish cleanup after recovery. Cross-region active/active remains out of
scope.
