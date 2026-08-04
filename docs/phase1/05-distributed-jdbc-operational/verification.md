# Slice 05 Verification

## Automated Checks

| Check | Result |
|---|---|
| `mvn.cmd -B -ntp verify -DskipITs` | PASS; all 29 reactor modules; 158 tests, 0 failures, 0 errors, 0 skipped |
| `mvn.cmd -B -ntp spotless:check` | PASS; all configured Java sources clean |
| `mvn.cmd -B -ntp -pl engine/coordinator -am test -Dtest=CoordinatorApplicationTest -Dsurefire.failIfNoSpecifiedTests=false` | PASS; two real TCP/H2 integration scenarios |
| `mvn.cmd -B -ntp -pl engine/coordinator,engine/worker -am package -DskipTests` | PASS; both shaded executable JARs created |
| Shaded manifest and `META-INF/services/java.sql.Driver` inspection | PASS; correct main classes and merged MySQL/PostgreSQL drivers in both JARs |
| `helm lint deployment/helm/astrasync -f deployment/helm/astrasync/examples/jdbc-values.yaml` | PASS; 0 chart failures |
| Long-release `helm template` with JDBC values | PASS; data-plane-only StatefulSet and revisioned Job with two stable endpoints |
| `docker compose -f deployment/docker/docker-compose.dev.yml config --quiet` | PASS |
| `docker compose -f deployment/docker/docker-compose.dev.yml build worker-0 coordinator` | BLOCKED; Docker Hub OAuth endpoint timed out before Maven or Temurin base-image metadata resolved |
| `git diff --check` | PASS |

## Acceptance Evidence

- `RemoteTaskFactoryTest` verifies bounded descriptor tasks, null and limit validation, and fail-fast
  placeholder resources that cannot execute on the Coordinator.
- `CoordinatorConfigurationTest` verifies environment defaults and overrides, endpoint parsing,
  duplicate Worker rejection, path normalization, and invalid configuration failures.
- `JdbcWorkerTaskFactoryProviderTest` loads a real JobSpec, materializes JDBC resources, executes a
  split through `InProcessBatchWorker`, writes H2 rows, and rejects missing, invalid, and non-JDBC
  configuration.
- `CoordinatorApplicationTest` starts two real TCP Worker services. It proves round-robin execution of
  two JDBC ranges, complete row transfer, and aggregate metrics. Its restart scenario waits until the
  first split is present in the atomic manifest before failing the second Worker, then proves that a
  fresh Coordinator executes only the unfinished split.
- The restart scenario also proves that a complete rerun materializes no remote task and that changed
  range boundaries are rejected before any additional Worker request.
- Helm rendering proves StatefulSet pod names are protocol Worker IDs, headless DNS names are stable,
  the Coordinator endpoint list includes every replica, both processes mount the same Secret, and
  only the Coordinator mounts the required progress claim.

## Known Limits

The local Docker daemon could parse the Compose model but could not build either Java image because
the Docker Hub authentication endpoint was unreachable. No local base image contained a Linux Java
21 runtime, so the containerized demo could not be executed without changing the production
Dockerfiles. The same two-process boundary is covered in Java by real TCP sockets and independent
Worker services.

Phase 1 still has a replay window after JDBC sink commit and before manifest replacement. It has no
intra-split checkpoint, transactional cross-split commit, task retry within one Coordinator
invocation, epoch fencing, leader election, authenticated transport, or dynamic Worker registry.
