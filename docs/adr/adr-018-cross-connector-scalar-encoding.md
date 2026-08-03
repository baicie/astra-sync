# ADR-018: Cross-connector Scalar Encoding

## Status

Accepted

## Date

2026-08-03

## Context

Slice 03 deliberately restricted the CSV Sink to `String` and explicit null values. Slice 04's
JDBC Source correctly returns typed `Row` values such as `Integer`, `BigDecimal`, `LocalDate`, and
`byte[]`; rejecting those values makes a JDBC-to-file job fail even though both connectors use the
same public SPI. A generic transform registry does not exist in Phase 0, so the boundary that
receives typed values must own a deterministic scalar encoding.

## Decision

As a Slice 04 additive extension, the CSV Sink accepts the JDBC canonical scalar matrix and emits
locale-independent text using these rules:

| Row value | CSV field text |
|---|---|
| `String` | unchanged |
| `Boolean` | `true` or `false` |
| `Byte`, `Short`, `Integer`, `Long`, `Float`, `Double`, `BigDecimal` | Java `toString()` decimal form |
| `LocalDate`, `LocalTime`, `LocalDateTime`, `OffsetDateTime` | ISO-8601 `toString()` form |
| `byte[]` | RFC 4648 standard Base64 without line breaks |
| `null` | the configured CSV `nullValue`; still rejected when no token is configured |

The CSV Source remains text-oriented: it does not infer these types on a later read. CSV quoting,
escaping, CRLF output, null-token ambiguity, and create-new publication remain the ADR-015
contract. Values outside this table still fail at `SINK_WRITE` with the column and Java type.

## Consequences

### Positive

- JDBC-to-file jobs work through the existing bounded runner without a hidden transform stage.
- Numeric, temporal, and binary output is deterministic across locales and JVMs.
- The original strict CSV contract remains intact for direct file-to-file paths.

### Negative

- CSV output is a textual representation and does not preserve JDBC types for a later CSV read.
- Binary consumers must decode Base64, and a future schema-aware format may choose a different
  representation.
- Adding another scalar type requires an explicit mapping update rather than implicit coercion.

## Alternatives Considered

### Add a Generic Transform Registry

Deferred because executable transforms are outside the Phase 0 compiler and would enlarge the
public SPI before the first JDBC path is proven.

### Convert Values in the JDBC Source

Rejected because it would make the JDBC connector lose native values for JDBC-to-JDBC jobs and
would couple source semantics to one text sink.

### Reject JDBC-to-file

Rejected because Phase 0 explicitly requires the same runtime and connector SPI to support both
directions.

## Related Decisions

- ADR-015: Strict CSV and Create-new Output
- ADR-016: JDBC Connector Contract and Type Mapping
- ADR-017: JDBC Transaction Boundaries
