# Phase 4 Slice 11 Implementation Plan

- [x] Define the canonical JobSpec, desired/observed states, checkpoint/failure status, and domain
      validation in the shared control-plane module.
- [x] Implement idempotent Start/Stop, legal observed-state transitions, epoch fencing, restart
      accounting, and active-job update/delete protection.
- [x] Implement thread-safe memory and PostgreSQL repositories with namespace pagination,
      optimistic versions, embedded migration, and durable reopen coverage.
- [x] Replace the placeholder Job protobuf with CRUD, lifecycle, status, and complete JobSpec
      messages; generate Go, gRPC, and grpc-gateway bindings.
- [x] Implement stable gRPC error mapping, JSON gateway, PostgreSQL startup, health/readiness, and
      graceful API shutdown.
- [x] Add REST-through-gRPC lifecycle coverage and concurrent duplicate-command idempotency tests.
- [x] Align the SyncJob API and generated CRD with the shared model; implement the idempotent
      reconciler and Lease-elected controller manager.
- [x] Repair Docker and Helm wiring for Go submodules, probes, namespace discovery, and leader
      election.
- [x] Test and vet every Go module; run PostgreSQL integration in CI; lint and drift-check generated
      protobuf and CRD artifacts.
- [x] Record the design, operational boundary, ADR, roadmap status, and verification procedure.
