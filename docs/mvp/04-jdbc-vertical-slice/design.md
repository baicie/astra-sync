# JDBC Vertical Slice Design

## Context

Slice 03 establishes a generic local runner and a strict file vertical path. The empty
`connector-jdbc` module already owns MySQL and PostgreSQL drivers but has no behavior. Slice 04
adds one generic JDBC implementation without leaking SQL, driver, or transaction details into the
Engine or changing the existing Source/Sink SPI.

## End-to-end Flow

```text
JobSpec -> JobCompiler -> CompiledJobPlan
                          |
                          v
            LocalJobRunner creates jdbc/csv instances
                          |
                          v
                 SingleNodeSyncJob (bounded pull)
                          |
        JdbcBatchSource -> JdbcBatchSink / CsvBatchSink
                          |
                          v
                   terminal metrics
```

Dependency direction remains:

```text
cli -> engine -> connector-api
  \-> connector-file -> connector-api
  \-> connector-jdbc -> connector-api + JDBC drivers
```

`connector-jdbc` never depends on Engine. Factory creation validates and normalizes string options
only; connections, statements, transactions, and result sets are acquired in `open`.

## Connector Descriptor and Configuration

`JdbcConnectorFactory` uses name `jdbc`, version `1.0.0`, roles `SOURCE` and `SINK`, and capabilities
`BATCH_READ` and `BATCH_WRITE`. It deliberately does not claim replayable offsets,
`TRANSACTIONAL_COMMIT`, idempotency, or stronger delivery guarantees.

Source options:

| Option | Rule |
|---|---|
| `url` | required, non-blank, must start with `jdbc:` |
| `query` | required, non-blank, one statement supplied by the caller |
| `user` | optional, passed to DriverManager |
| `password` | optional, passed to DriverManager and never logged |
| `fetchSize` | optional positive integer; default `0` delegates to the driver |
| `queryTimeoutSeconds` | optional positive integer; default `0` means no timeout |

Sink options:

| Option | Rule |
|---|---|
| `url` | required, non-blank, must start with `jdbc:` |
| `table` | required, one or two unquoted identifier segments |
| `user` | optional, passed to DriverManager |
| `password` | optional, passed to DriverManager and never logged |
| `queryTimeoutSeconds` | optional positive integer; default `0` means no timeout |

Unknown options fail before any JDBC call. Table segments use `[A-Za-z_][A-Za-z0-9_]*`; database
identifier quoting is selected from `DatabaseMetaData` and applied to table and Row column names.
Source labels are case-sensitive and must be non-blank and unique. The destination table and
columns must already exist with compatible types.

## Source Lifecycle

`open` obtains a `Connection`, sets it read-only with autocommit disabled, configures the forward-
only read-only statement, applies fetch size and timeout, executes the configured query, and
captures `ResultSetMetaData` labels/types. It validates labels before exposing the Source as open.

`readBatch(maxRows)` calls `ResultSet.next()` only until `maxRows` rows are materialized or the
result is exhausted. It returns a terminal batch on the first pull that observes EOF and does not
look ahead after an exact batch boundary. Each value is converted using the ADR-016 matrix; CLOBs
and BLOBs are fully materialized, and binary values are copied before entering `Row`.

`close` rolls back the read-only transaction, closes ResultSet, Statement, and Connection, and
preserves a primary failure with close failures suppressed. Calls outside the lifecycle are
rejected deterministically.

## Sink Lifecycle

`open` obtains a Connection, disables autocommit, and leaves insert statement creation deferred
until the first non-empty batch supplies an ordered column set. For that first batch it builds a
parameterized `INSERT INTO <quoted-table> (<quoted-columns>) VALUES (?,...)` statement. It never
concatenates row values into SQL.

Each `writeBatch` validates the complete column-name set, binds only supported Row values, calls
`executeBatch`, and then commits exactly once. An execute or commit failure attempts rollback and
propagates a runtime error; `close` rolls back any pending transaction and never commits. Later
batches keep the first header order even when their Row maps use another insertion order.

## Type Matrix and Failure Evidence

The exact source mapping is defined in ADR-016. Unsupported vendor objects fail before the row is
returned with the connector path, column label, JDBC type name, and record number. Sink conversion
errors identify the column and Java type and occur before execute for that row. No implicit
`String.valueOf`, locale conversion, or silent null replacement is allowed.

## Test Architecture

Connector tests use H2 in-memory databases in test scope. Each test owns a unique database name,
creates its tables, and closes the connection; no Docker or external service is required. Tests
cover metadata order, bounded pulls, all supported types, duplicate/blank labels, unsupported
types, transaction commit/rollback, resource closure, and schema drift.

CLI tests register the JDBC factory beside CSV, create an H2 source/target, and exercise both
JDBC-to-file and file-to-JDBC through the same LocalJobRunner. Production CLI packaging includes
the JDBC connector and merges JDBC driver service descriptors.

## Change Control

Adding parameters to source SQL, accepting raw SQL/table fragments, auto-creating schemas,
supporting vendor-specific values, parallel reads, retries, upserts, or stronger transaction and
delivery guarantees requires a design update and a new or superseding ADR before implementation.
