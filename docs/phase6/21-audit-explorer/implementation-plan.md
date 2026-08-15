# Phase 6 Slice 21 Implementation Plan

## 1. Contract and Domain

- [x] Add the protobuf-first `AuditService` list contract and generated Go bindings.
- [x] Define bounded query, cursor, event, and repository types in `control-plane/auth`.
- [x] Document tenant isolation, projection allowlist, token fencing, and failure behavior.

## 2. Persistence and API

- [x] Add the exact tenant/time/keyset query index and PostgreSQL repository implementation.
- [x] Implement time/filter validation, HMAC tokens, safe attribute projection, and read auditing.
- [x] Register the service with gRPC, REST gateway, and the deny-by-default authorization registry.
- [x] Cover direct authorization, cross-tenant isolation, malformed tokens, pagination, redaction,
  and audit-write failure.

## 3. Console

- [x] Add the authenticated BFF route with server-derived tenant scope.
- [x] Add the permission-aware activity view, filters, detail inspection, and older-page loading.
- [x] Cover tenant selection, permission denial, escaped rendering inputs, and responsive layout.

## 4. Closeout

- [x] Run protobuf lint/generation checks, Go format/vet/test, PostgreSQL integration tests, and
  relevant repository checks.
- [x] Record verification evidence and update the Phase 6 and ADR indexes.
