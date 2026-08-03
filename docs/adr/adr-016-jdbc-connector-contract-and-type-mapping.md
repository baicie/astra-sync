# ADR-016: JDBC Connector Contract and Type Mapping

## Status

Accepted

## Date

2026-08-03

## Context

Slice 03 proves the compile-before-open and bounded Source/Sink lifecycle with CSV, but the
JDBC module is still an empty shell. JDBC exposes vendor-specific Java objects, driver-managed
resources, and SQL identifiers that cannot be left to incidental `toString()` or string
concatenation behavior. Phase 0 needs one generic connector that can exercise JDBC-to-JDBC and
file/JDBC paths without adding a JDBC-specific API to the Engine.

## Decision

The `connector-jdbc` module provides one explicit `JdbcConnectorFactory` with descriptor name
`jdbc`, version `1.0.0`, both `SOURCE` and `SINK` roles, and only `BATCH_READ`/`BATCH_WRITE`
capabilities. It depends on `connector-api` and owns its JDBC driver/SQL logic; the Engine has no
dependency on this module. The CLI explicitly registers both CSV and JDBC factories.

The resource-free configuration contract is:

| Role | Required options | Optional options | Rules |
|---|---|---|---|
| Source | `url`, `query` | `user`, `password`, `fetchSize`, `queryTimeoutSeconds` | query is non-blank; numeric options are positive when present; defaults are driver fetch behavior and no timeout |
| Sink | `url`, `table` | `user`, `password`, `queryTimeoutSeconds` | table is one or two unquoted identifier segments; timeout is positive when present |

Unknown options are rejected. Option values and JDBC URLs are never included in configuration
`toString()` or normal CLI messages. `DriverManager` selects a driver from the connector's
declared JDBC dependencies. The CLI shaded JAR merges `META-INF/services/java.sql.Driver`
resources so more than one bundled driver remains discoverable.

The connector uses the following canonical row value matrix. Source values are converted before
they enter `Row`; `byte[]` values are defensively copied. Sink values are bound with
`PreparedStatement.setObject` after validating they belong to this matrix.

| JDBC family | Row value |
|---|---|
| `NULL` | `null` |
| `CHAR`, `VARCHAR`, `LONGVARCHAR`, `NCHAR`, `NVARCHAR`, `LONGNVARCHAR`, `CLOB`, `SQLXML` | `String` |
| `BOOLEAN`, `BIT` | `Boolean` |
| `TINYINT`, `SMALLINT`, `INTEGER`, `BIGINT` | `Byte`, `Short`, `Integer`, `Long` respectively |
| `REAL`, `FLOAT`, `DOUBLE` | `Float`, `Double`, `Double` respectively |
| `DECIMAL`, `NUMERIC` | `BigDecimal` |
| `DATE`, `TIME`, `TIMESTAMP` | `LocalDate`, `LocalTime`, `LocalDateTime` |
| `TIMESTAMP_WITH_TIMEZONE` | `OffsetDateTime` |
| `BINARY`, `VARBINARY`, `LONGVARBINARY`, `BLOB` | copied `byte[]` |

Other SQL types, vendor objects, arrays, and references fail at `SOURCE_READ` with the column
label and JDBC type name. Sink values outside the matrix fail at `SINK_WRITE`; no locale-sensitive
or implicit text coercion is performed. Source metadata uses `ResultSetMetaData.getColumnLabel`,
preserves order, and rejects blank or duplicate labels before the first batch is returned.

The sink accepts a simple table or schema-qualified table identifier. It obtains the database
identifier quote string, quotes table and row column identifiers, derives the insert statement
from the first non-empty batch, and keeps that column order for every later batch. Empty input
therefore performs no insert and does not infer a schema.

## Consequences

### Positive

- JDBC behavior is bounded by the existing batch SPI and remains out of the Engine.
- Decimal, temporal, binary, null, Unicode, and large-text behavior is explicit and testable.
- Metadata and identifier validation prevents silent column reordering and common SQL injection
  mistakes in generated insert statements.
- Multiple JDBC drivers can coexist in the CLI artifact without service-resource loss.

### Negative

- Vendor-specific types such as arrays, JSON objects, spatial values, and proprietary intervals
  are rejected until a versioned mapping is designed.
- Generic table insertion requires the destination schema to already exist and match the Row
  column set; schema creation and evolution are out of scope.
- Driver behavior still influences fetch performance and exact temporal precision within the
  documented JDBC type matrix.

## Alternatives Considered

### Convert Every Value to String

Rejected because it loses decimal scale, temporal semantics, binary bytes, and null fidelity.

### Pass Vendor Objects Directly Through Row

Rejected because values would not be portable across drivers and could retain live JDBC resources.

### Accept Raw Table or SQL Fragments

Rejected because unquoted fragments make identifier handling and secret-safe diagnostics
unreliable. A future explicit `insertSql` contract can supersede this restriction.

## Related Decisions

- ADR-008: Dual Format Support (Row and Arrow)
- ADR-009: Exactly-Once via Capability Negotiation
- ADR-011: Bounded Pull-based Single-node Runtime
- ADR-013: Descriptor-first Connector Planning
- ADR-014: Local Runner and CLI Boundary
