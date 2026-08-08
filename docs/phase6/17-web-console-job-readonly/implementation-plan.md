# Phase 6 Slice 17 Implementation Plan

## 1. Contract and boundary

- [x] Record the Console topology, HTTP contract, namespace scope, and non-goals.
- [x] Add ADR-035 for the namespace-scoped read-only adapter.
- [x] Correct the root roadmap to show Phase 5 complete and Phase 6 in progress.

## 2. Console runtime

- [x] Add a Go Console module with configurable gRPC endpoint, HTTP listen address, and namespace.
- [x] Serve an embedded responsive Job list/detail UI.
- [x] Implement health, readiness, list, detail, and status endpoints over the existing JobService.
- [x] Translate upstream gRPC failures and enforce server-side namespace scope.

## 3. Verification and delivery

- [x] Test configuration, namespace enforcement, health/readiness, list/detail/status responses,
  upstream failures, and the absence of mutation routes.
- [x] Run focused Go tests, repository diff checks, and the applicable Maven verification.
- [x] Record verification evidence and deliver through a squash-merged pull request.
