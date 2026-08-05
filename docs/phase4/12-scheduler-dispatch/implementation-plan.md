# Phase 4 Slice 12 Implementation Plan

## 1. Align Execution Epochs

- Add optional Coordinator configuration for an externally allocated execution epoch.
- Extend the checkpoint store contract with idempotent external epoch acquisition.
- Preserve automatic epoch allocation for local and legacy deployment paths.
- Test accepted retries, stale epochs, skipped epochs, and environment validation.

## 2. Add Durable Scheduler Admission

- Add the dispatch migration and phase/identity model.
- Serialize global capacity decisions with a PostgreSQL transaction advisory lock.
- Renew owned rows, take over expired rows, and guard every update by owner and lease.
- Prove no over-admission and same-epoch takeover against a real PostgreSQL service.

## 3. Reconcile Desired State

- Poll durable `RUNNING/INITIALIZING` Jobs and reconcile claimed executions concurrently.
- Map pending/running/completed/failed observations to legal epoch-fenced lifecycle transitions.
- Delete and observe resources before completing cancellation.
- Separate transient infrastructure errors from permanent materialization failures.

## 4. Dispatch Kubernetes Resources

- Generate strict JobSpec documents into immutable Secrets.
- Create deterministic Worker Service/StatefulSet and wait for readiness.
- Create the one-shot Coordinator Job with authoritative epoch and isolated progress path.
- Retain terminal Coordinator Jobs while cleaning execution-scoped auxiliary resources.
- Reject identity/fingerprint collisions and unresolved connection references.

## 5. Operationalize and Verify

- Add Scheduler configuration, process health/readiness, graceful shutdown, and container image.
- Add Helm Deployment, progress PVC, PDB, RBAC, and dynamic/manual mode validation.
- Expand CI with Scheduler PostgreSQL integration and all runtime image builds.
- Record design, operations, limitations, and verification evidence in Slice 12 docs and ADR-030.
- Run all Go modules, PostgreSQL integration, Maven/Spotless, protocol/CRD drift, Helm, Docker, and
  repository policy checks before squash merge.
