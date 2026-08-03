# Slice 04 Implementation Plan

## Preconditions

- [x] Start from the verified Slice 03 documentation commit `f7cfbc1` (implementation `f7c6b3c`).
- [x] Re-read the supplied architecture, Phase 0 plan, ADR-004/009/011/013/014/015.
- [x] Inventory the empty `connector-jdbc` module, JDBC driver ownership, and CLI composition.
- [x] Define the JDBC option, identifier, type, and transaction contracts in ADR-016/017 before
  changing Java or Maven files.

## Steps

### 1. Establish JDBC Contract and Dependencies

- Add the `JdbcConnectorFactory`, option parser, descriptor, and strict resource-free validation.
- Add the H2 test dependency without adding test infrastructure to production runtime.
- Gate: factory and option tests prove unknown/invalid values fail before DriverManager access and
  `toString()` does not reveal passwords, URLs, queries, or tables.

### 2. Implement the JDBC Source

- Add one-connection read-only transaction ownership with forward-only statement configuration.
- Capture and validate ordered metadata labels before the Source becomes open.
- Implement bounded reads and the ADR-016 type matrix, including defensive LOB/binary materialization.
- Gate: H2 source tests prove exact batch limits, terminal behavior, metadata errors, supported
  values, unsupported type errors, and close/rollback behavior.

### 3. Implement the JDBC Sink

- Add identifier validation and database-specific identifier quoting.
- Derive one parameterized insert from the first non-empty Row and enforce the column set.
- Bind supported values, execute each batch, commit once, rollback on failure, and close without
  committing.
- Gate: H2 sink tests prove commit/rollback boundaries, schema drift rejection, type binding,
  unsupported values, empty input, and resource cleanup.

### 4. Integrate CLI and Cross-Connector Paths

- Register `JdbcConnectorFactory` in the CLI and add JDBC driver service-resource merging to the
  shaded artifact.
- Add H2-backed CLI tests for JDBC-to-file and file-to-JDBC using the same local runner and stable
  exit categories; retain CSV behavior unchanged.
- Gate: packaged CLI help/version and focused file/JDBC acceptance tests pass without exposing
  credentials.

### 5. Verify and Archive

- Run focused connector/engine/CLI verification with JaCoCo gates.
- Run the full Reactor test suite, Spotless, diff, dependency-direction, driver-service, and
  packaged-JAR checks.
- Record exact commands, test counts, type/transaction evidence, coverage, limitations, and the
  resulting implementation commit in `verification.md`.
- Update Slice 04 and Phase 0 status only after every acceptance item passes.
- Gate: worktree is clean and no generated H2/JDBC artifacts are tracked.

## Change Control

Do not add schema migration, query parameters, vendor-specific SQL, parallel splits, retries,
upserts, cleanup of committed batches, or stronger delivery claims in this slice without revising
the design and ADRs first.
