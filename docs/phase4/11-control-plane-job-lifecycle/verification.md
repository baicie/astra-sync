# Phase 4 Slice 11 Verification

## Automated Checks

| Check | Result |
|---|---|
| Shared Job domain, lifecycle, memory repository, and PostgreSQL package tests | PASS |
| API service, REST-through-gRPC, server health/readiness, and error mapping tests | PASS |
| Controller API and desired-state reconciler tests | PASS |
| PostgreSQL lifecycle integration with `ASTRASYNC_TEST_POSTGRES_URL` | PASS; PostgreSQL 17 container, test not skipped |
| All six Go modules: formatting, build, `go test ./...`, and `go vet ./...` | PASS |
| Buf lint and deterministic Go protobuf regeneration | PASS; no generated diff |
| Controller-gen deterministic SyncJob CRD regeneration | PASS; no generated diff |
| Helm chart lint | PASS |
| Control-plane Docker image builds | CI job added; local Docker Hub authorization blocked before base-image resolution |
| Full Maven reactor verification and Spotless | PASS; 30 modules |
| `git diff --check` | PASS |

## Acceptance Evidence

- Domain tests cover canonical model validation, defensive copies, legal transitions, terminal
  failure rules, restart epochs, stale-epoch rejection, and active-job mutation protection.
- Repository tests cover duplicate create, stable namespace pages, invalid pages, optimistic update
  and delete conflicts, copies, PostgreSQL migration, reopen recovery, and an empty page retaining
  the correct total.
- Service tests cover CRUD, pagination, error codes, runtime defaults, version conflicts,
  idempotent retries, and a simulated concurrent duplicate Start that commits before returning a
  conflict.
- The server integration test sends JSON through grpc-gateway, an in-memory gRPC transport, the
  generated client/server contract, JobService, and the repository.
- Controller tests prove each desired-state change is applied once and unknown desired states are
  rejected.
- CI starts PostgreSQL 16, runs the non-skipped persistence test, and rejects protobuf or CRD
  generation drift.

## Final Gate

Run from the repository root:

```text
make test-go
make vet-go
buf lint api/protobuf
buf generate api/protobuf --template buf.gen.yaml
make crd-generate
mvn -B -ntp verify -DskipITs
mvn -B -ntp spotless:check
helm lint deployment/helm/astrasync
git diff --check
```

The PostgreSQL integration test additionally requires `ASTRASYNC_TEST_POSTGRES_URL`. CI supplies a
real database and treats a skip as insufficient evidence. The `container-images` CI job builds both
control-plane images without pushing them; the local verification environment could not reach
Docker Hub's IPv6 OAuth endpoint.
