# Phase 4 Slice 12 Verification

## Automated Checks

| Check | Result |
|---|---|
| Coordinator configuration and checkpoint external-epoch tests | PASS |
| Scheduler lifecycle and permanent/transient error tests | PASS |
| Kubernetes deterministic create, readiness, completion, cleanup, and Stop tests | PASS |
| PostgreSQL capacity, lease takeover, stale-owner fencing, and capacity release integration | PASS; PostgreSQL 16 container, test not skipped |
| Scheduler `go test ./...` and `go vet ./...` | PASS |
| Helm lint and default/manual mode rendering | PASS |
| All six Go modules: tests, vet, and changed-file formatting | PASS |
| Full Maven reactor verification and Spotless | PASS; 30 modules |
| Buf lint/generation and controller-gen CRD drift | PASS; pinned CI versions, no generated diff |
| API, Controller, Scheduler, Coordinator, and Worker image builds | PASS |
| Helm default dynamic mode and legacy manual mode rendering | PASS |
| `git diff --check` | PASS |

## Acceptance Evidence

- Scheduler tests prove a completed Coordinator advances an initializing Job through Running to
  Finished, failure details are persisted, cancellation waits for dispatcher Stop, and a permanent
  JobSpec rejection terminates the epoch rather than retrying forever.
- Kubernetes fake-client tests prove Secret/Service/StatefulSet creation is idempotent, the
  Coordinator waits for Worker readiness, the control-plane epoch is injected, Job completion is
  observed, auxiliary resources are cleaned, Stop removes the complete group, and `connectionRef`
  fails before any API mutation.
- The PostgreSQL test opens independent connection pools, admits only one of two pending Jobs,
  refuses spare capacity while its lease is live, transfers the exact UID/epoch after expiry,
  rejects the stale owner, and admits the second Job only after terminal release.
- File checkpoint tests prove one external epoch can be reacquired without incrementing and stale
  or skipped epochs cannot become active.

## Final Gate

Run from the repository root:

```text
for each Go module: gofmt, go test ./..., go vet ./...
ASTRASYNC_TEST_POSTGRES_URL=... go test -count=1 ./control-plane/scheduler/internal/dispatch/postgres
mvn -B -ntp verify -DskipITs
mvn -B -ntp spotless:check
buf lint api/protobuf
buf generate api/protobuf --template buf.gen.yaml
controller-gen CRD regeneration and git diff check
helm lint deployment/helm/astrasync
docker build each API/Controller/Scheduler/Coordinator/Worker Dockerfile
git diff --check
```

CI supplies PostgreSQL 16 and treats the integration as required rather than accepting a skip.
Local Docker Hub OAuth was unreachable over IPv6, so the same official base image tags and digests
were pulled through a registry proxy and all five Dockerfiles were then built with `--pull=false`.
CI independently resolves the official registry.
