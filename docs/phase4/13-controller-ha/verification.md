# Phase 4 Slice 13 Verification

## Local automated checks

Executed on 2026-08-06 before opening the pull request.

| Check | Result |
|---|---|
| Controller convergence, conflict retry, inactive refresh, active spec replacement, replacement-Reconciler projection, rich status, and finalizer tests | PASS |
| Scheduler lifecycle, heartbeat CAS race, post-fence owner recovery, and Kubernetes observation separation | PASS |
| Kubernetes heartbeat materialization, deterministic adoption, terminal cleanup, and safe orphan sweep | PASS |
| Scheduler heartbeat HTTP authentication and configuration validation | PASS |
| Coordinator initial/periodic authenticated heartbeat and rejection tests | PASS; Coordinator reactor 18 tests |
| All six Go modules: `go test -count=1 ./...` and `go vet ./...` | PASS |
| Full Java reactor `mvn -B -ntp verify -DskipITs` | PASS; 30 modules |
| Full Java reactor `mvn -B -ntp spotless:check` | PASS |
| Buf 1.34 lint/generation with pinned Go plugins | PASS; no generated Go diff |
| controller-gen 0.15 CRD regeneration under Go 1.22.12 | PASS; no generated CRD diff |
| Helm default dynamic mode and legacy manual mode lint/render | PASS |
| `git diff --check` | PASS |

The Windows checkout retains pre-existing CRLF files that local `gofmt -l .` reports even though the
Git objects are formatted. Every changed Go file was formatted directly; Linux CI performs the
authoritative clean-checkout format gate.

## Pull-request-only gates

The following require services unavailable in the local environment and remain required before
merge:

| Check | Required evidence |
|---|---|
| PostgreSQL lifecycle repository integration | PostgreSQL 16 CI service, test not skipped |
| Scheduler two-pool capacity, lease/heartbeat takeover, token rejection, and atomic timeout fence | PostgreSQL 16 CI service, test not skipped |
| Controller real-PostgreSQL convergence and replacement status projection | PostgreSQL 16 CI service, test not skipped |
| API, Controller, Scheduler, Coordinator, and Worker images | All five Buildx matrix jobs pass |
| Repository secret and policy checks | Required repository job passes |

## Acceptance evidence

- Controller tests prove the CRD spec and desired state converge into PostgreSQL with optimistic
  retries, active spec changes stop before replacement and restart at the next epoch, a fresh
  Reconciler projects durable Scheduler state without local history, and deletion waits for terminal
  state before deleting the row and finalizer.
- Coordinator and Scheduler endpoint tests prove an execution-scoped UUID token is sent before work
  and periodically thereafter, malformed or stale credentials are rejected, and Kubernetes Job
  activity does not forge liveness.
- Scheduler tests prove a heartbeat received after a stale snapshot wins the PostgreSQL-style CAS,
  while a durable `STOPPING` timeout fence is resumed after the original owner exits and completes as
  `HeartbeatTimeout` without allocating a new UID/epoch.
- PostgreSQL integration covers independent connection pools, global capacity, live lease renewal,
  stale-heartbeat takeover, stale-owner rejection, fresh-heartbeat CAS protection, timeout fencing,
  terminal token rejection, lease takeover, and repeatable namespace cleanup.
- Kubernetes fake-client tests prove active identities retain all resources, terminal identities
  retain only Coordinator Jobs, absent identities are fully swept, and unmanaged or malformed-label
  resources are preserved.

## Final gate

The pull request must pass every required GitHub Actions job. In particular, a skipped PostgreSQL
test is not accepted as integration evidence, and container success is required for all five runtime
images before squash merge.
