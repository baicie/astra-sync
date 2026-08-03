# Phase 0 Examples

These examples exercise the synchronous single-process MVP. Run commands from the repository
root after packaging the CLI:

```powershell
mvn.cmd -pl cli -am package -DskipTests
java -jar cli/target/astrasync-cli-0.1.0-SNAPSHOT-all.jar run examples/phase0/csv/job.yaml
java -jar cli/target/astrasync-cli-0.1.0-SNAPSHOT-all.jar run --metrics json examples/phase0/csv/job.yaml
```

The CSV example is self-contained. It creates `examples/phase0/csv/output.csv` with create-new
semantics; remove that file before repeating the command. The input and output schemas are
inferred from the first row, and the configured `maxBatchRecords` is the upper bound for every
pull and write.

## JDBC

The JDBC connector accepts a `jdbc:` URL, optional `user` and `password`, and either a source
`query` or a sink `table`. Sink tables must already exist. A source uses a read-only transaction;
the sink commits each batch and rolls back the current batch if its write fails. The supported
scalar and temporal mappings are recorded in [ADR-016](../../docs/adr/adr-016-jdbc-connector-contract-and-type-mapping.md).

H2 is a test-scoped dependency and is not a production runtime prerequisite. The repeatable JDBC
example is the integration test suite:

```powershell
mvn.cmd -pl cli -am test -DskipITs -Dtest=JdbcCliIntegrationTest -Dsurefire.failIfNoSpecifiedTests=false
```

Production JDBC runs must provide the matching driver and a pre-created schema. Do not put
credentials in a checked-in JobSpec or in a metrics report.

## Delivery Boundary

Phase 0 is synchronous and at-most-once. A source or sink failure can leave partial output, and a
successful batch cannot be undone by a later cancellation. Cancellation is cooperative and is
observed between bounded connector calls; it does not interrupt a driver call already in progress.
Use `--metrics json` for machine-readable counters and failure stages. Reports intentionally omit
connector options, passwords, SQL, paths, and exception causes.
