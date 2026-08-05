# Phase 3 CDC Verification

## Automated Checks

| Check | Result |
|---|---|
| Connector API, Debezium, MySQL CDC, and PostgreSQL CDC focused tests | PASS; 43 tests run, 0 failures and 0 errors |
| MySQL CDC Testcontainers integration test | SKIPPED; Docker daemon unavailable in the verification environment |
| PostgreSQL CDC Testcontainers integration test | SKIPPED; Docker daemon unavailable in the verification environment |
| Full Maven test gate (`mvn clean test`) | PASS; all 30 Reactor modules succeeded; the two database integration tests were skipped because Docker was unavailable |
| Spotless formatting check (`mvn spotless:check`) | PASS; all configured Java sources clean |
| `git diff --check` | PASS |

## Acceptance Evidence

- `CdcContractsTest` verifies immutable event, key, position, and batch contracts.
- `DebeziumRecordConverterTest` verifies snapshot, update, tombstone, heartbeat, source metadata,
  and row conversion.
- `OffsetStateCodecTest` verifies deterministic opaque offset round-tripping and connector/format
  rejection.
- MySQL and PostgreSQL factory tests verify connector properties, protected-option rejection, and
  resource-free source construction.
- The JDBC CDC sink tests verify insert/update/delete behavior, marker creation, repeated commit
  no-op, and digest conflict rejection.
- Worker tests verify sink commit -> source acknowledgement -> checkpoint ordering and the absence
  of source acknowledgement after a sink failure.
- Coordinator tests verify durable offset recovery in a new execution epoch.
- The CLI ServiceLoader test verifies that `mysql-cdc`, `postgres-cdc`, and `jdbc` factories are
  discoverable on the packaged command classpath.
- Testcontainers tests exercise a real snapshot followed by acknowledged insert/update/delete
  changes when a Docker daemon is available.

## Delivery Boundary

The implementation is fully compiled and covered by self-contained tests in this environment. The
two database tests are present and compile successfully, but could not start their MySQL and
PostgreSQL containers because Docker was unavailable. Remote Worker CDC transport and production
multi-task CDC scheduling remain outside Phase 3.

## Final Gate

Run from the repository root:

```text
mvn clean test
mvn spotless:check
git diff --check
```

All commands must complete successfully before merging. A Docker-enabled environment should rerun
the two skipped integration tests before production rollout.
