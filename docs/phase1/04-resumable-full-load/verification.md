# Slice 04 Verification

## Automated Checks

| Check | Result |
|---|---|
| `mvn.cmd -B -ntp -pl engine/checkpoint,engine/coordinator,engine/worker -am verify -DskipITs` | PASS; focused tests and JaCoCo checks |
| `mvn.cmd -B -ntp verify -DskipITs` | PASS; full Maven reactor |
| `mvn.cmd -B -ntp spotless:check` | PASS; all configured Java sources clean |
| `helm lint deployment/helm/astrasync` | PASS; 0 chart failures |
| `helm template astrasync deployment/helm/astrasync --set worker.enabled=true --set worker.taskFactoryProvider=example.Provider` | PASS; Worker Deployment and Service render |
| Worker shaded-JAR manifest inspection | PASS; `Main-Class` is `io.astrasync.engine.worker.WorkerApplication` |
| `docker build -f deployment/docker/Dockerfile.worker -t astrasync-worker:slice04 .` | BLOCKED; Docker Hub OAuth endpoint timed out before either base image was resolved |
| `git diff --check` | PASS; no whitespace errors |

## Evidence

- `FileSplitProgressStoreTest` verifies persistence and reload, first-success-wins duplicates,
  deterministic fingerprints, plan and descriptor drift rejection, missing state, and corrupt state.
- `ResumableBatchCoordinatorTest` fails a run after one durable success, creates a new Coordinator,
  verifies that only unfinished splits are materialized, and then verifies a fully complete rerun does
  no work.
- `BatchCoordinatorTest` verifies that success callbacks run in task-completion order while returned
  results retain deterministic task order. `ResumableBatchCoordinatorTest` also proves queued work is
  not started after the failure latch is set.
- `WorkerServiceTest` loads a real provider, starts the TCP server on a loopback port, executes a remote
  task through `WorkerClient`, verifies TCP health, closes the service, and verifies health failure.
- Helm renders the real protocol port and TCP probes only when a provider-backed Worker is enabled.

## Known Limits

This is split-level restart, not a consistency checkpoint. A crash after sink success but before
manifest persistence can replay a split. Failed splits restart from their original boundaries. The
file store requires a durable volume with file locking and atomic rename and supports only one active
Coordinator per job. There is no epoch fencing, automatic retry loop, transactional sink commit,
manifest cleanup API, TLS/authentication, Worker registration, or bundled connector provider plugin.
