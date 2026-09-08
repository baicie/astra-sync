# Phase 24: Emission Sub-Slice 43.1.5 — apiserver_session_revoke_total via API Server revoke RPC

## Status

**Complete.** Phase 24 closes the emission sub-slice 43.1.5:
the API Server `RevokeConsoleSession` gRPC handler now invokes
`authpostgres.Repository.RevokeConsoleSessionsForPrincipal` in a
serializable transaction and emits
`apiserver_session_revoke_total` once per unique active tenant the
target principal holds a membership in. The architecture decision is
recorded in ADR-068. All slices (50.0–50.4) are landed; the Phase 24
acceptance criteria are fully satisfied.

## Theme

Complete the `apiserver_session_revoke_total` emission (Phase 17
activation half → Phase 24+ emission half). Phase 24 closes slice
43.1.5: wires the API Server revoke RPC to the metrics Recorder and
replaces the offline admin CLI as the canonical platform-level path.

## Scope

### Slices

| Slice | Owner | Description | Status |
|---|---|---|---|
| 50.0 | agent | ADR-068 + ADR index entry | **Done** |
| 50.1 | agent | Protobuf surface: `RevokeConsoleSession` RPC + request/response messages; regenerate Go bindings | **Done** |
| 50.2 | agent | AccessService handler: validation, platform-admin gate, audit-event construction, repository invocation, per-tenant Recorder emission | **Done** |
| 50.3 | agent | Wire `metrics.DefaultRecorder()` via `service.WithAccessRevokeRecorder` in `cmd/server/main.go` | **Done** |
| 50.4 | agent | Update `metrics-catalog.md` `apiserver_session_revoke_total` row + activation table | **Done** |
| 50.5 | agent | Unit tests: 8 cases (admin gate, per-tenant emission, idempotency validation, malformed principal, repository error sanitization, audit failure sanitization, unauthenticated, nil request) | **Done** |
| 50.6 | agent | Gate: `go vet`, `go test`, `go vet` for `control-plane/auth`, `check-changelog` | **Done** |

## Acceptance Criteria

- [x] ADR-068 written, indexed in `docs/adr/README.md`, status = Accepted
- [x] `RevokeConsoleSession` RPC + request/response messages added to `api/protobuf/v1/access.proto`
- [x] Go bindings regenerated and committed
- [x] `AccessService.RevokeConsoleSession` handler validates request, enforces platform-admin, builds `access.console_session.revoked` audit event
- [x] `auth.AccessRepository` extended with `RevokeConsoleSessionsForPrincipal`
- [x] `authpostgres.Repository.RevokeConsoleSessionsForPrincipal` writes audit row in the same `sql.LevelSerializable` transaction as the session DELETE
- [x] `service.WithAccessRevokeRecorder` functional option wired through `NewAccessService`
- [x] `cmd/server/main.go` passes the existing `metricRecorder` to `NewAccessService`
- [x] Authn interceptor registers `AccessService_RevokeConsoleSession_FullMethodName` policy
- [x] `apiserver_session_revoke_total` emitted once per unique active tenant
- [x] `metrics-catalog.md` row updated to reflect the production call site
- [x] `CHANGELOG.md` updated under `[Unreleased]` / `Added`
- [x] All `go vet` and `go test ./...` runs green for `control-plane/api-server` and `control-plane/auth`

## Out of Scope

- The `controller_epoch_fence_total` emission (slice 49.3.5) remains Phase 23+ pending ADR-053 §3.
- The Java data-plane `26.F9` emission remains pending ADR-051 §7.
- A Console BFF forwarder for `RevokeConsoleSession` is explicitly rejected (ADR-058 §2): the API Server gRPC handler is the canonical platform-level path.

## References

- ADR-012 — Strict Versioned JobSpec Boundary
- ADR-037 — Transactional Control-plane Audit Trail
- ADR-038 — Desired-state Job Mutation Workflows
- ADR-058 — Observability Catalog Backlog (Phase 17 umbrella)
- ADR-065 — Phase 22 admin-CLI session-revoke emission
- ADR-068 — Phase 24 API Server session-revoke emission (this phase)
