# ADR-017: JDBC Transaction Boundaries

## Status

Accepted

## Date

2026-08-03

## Context

The public `BatchSink` lifecycle has `open`, `writeBatch`, and `close`, but no success/abort or
commit-handle operation. JDBC still needs an honest boundary around writes: leaving autocommit on
would make a multi-row batch partially visible, while committing in `close` would publish data
after a source or transform failure. The Phase 0 runtime also has no checkpoint or replay loop.

## Decision

The JDBC Source opens one connection and one forward-only read-only transaction for the lifetime
of the source. It configures the requested fetch size and query timeout, executes one query, and
reads at most the requested number of rows per `readBatch` call. On normal or failed close it
rolls back the read-only transaction and closes ResultSet, Statement, and Connection in that
order. This supplies a single transaction boundary without claiming repeatable-read isolation;
the database's configured isolation level remains authoritative.

The JDBC Sink opens one connection with `autoCommit=false`. The first non-empty batch prepares a
parameterized insert. Each `writeBatch` executes all rows, calls `commit()` only after the batch
has executed successfully, and calls `rollback()` when execution or commit fails before
propagating a `SINK_WRITE` failure. A later `close` rolls back any uncommitted work and closes
PreparedStatement and Connection. No commit is performed by `close`.

This yields per-batch atomicity where the database and driver honor transactions, but it is not
end-to-end exactly-once: a commit can succeed before a client-side exception, and the runtime has
no checkpoint or replay coordination. The descriptor therefore does not advertise
`TRANSACTIONAL_COMMIT`, `IDEMPOTENT_WRITE`, or replay capabilities. Requested `at-least-once` and
`exactly-once` guarantees continue to fail compilation as required by Phase 0.

Integration tests use an isolated in-memory H2 database in the connector and CLI test scopes.
They create and drop their own schema, exercise the complete value matrix, verify source and sink
transaction effects, and require no Docker or external service. Production runtime dependencies
remain the MySQL and PostgreSQL JDBC drivers already owned by `connector-jdbc`.

## Consequences

### Positive

- A successful batch is committed as one database transaction, and failed batches are rolled back.
- Source and Sink ownership follows the existing kernel lifecycle and reverse-close behavior.
- Tests are reproducible from a clean checkout without a running database service.
- Delivery claims remain consistent with the missing checkpoint/commit coordinator.

### Negative

- A large job creates one committed transaction per runtime batch, not one job-wide transaction.
- A failure after a successful commit leaves already committed rows; rerunning into a non-idempotent
  table can duplicate them.
- Holding a read-only transaction for a full scan can retain a database snapshot and locks; this is
  documented operational behavior, not a hidden retry mechanism.

## Alternatives Considered

### Commit Only in `close`

Rejected because `close` runs after both successful and failed jobs and cannot distinguish them.

### Autocommit Every Row

Rejected because it destroys batch atomicity and makes partial writes harder to diagnose.

### Claim Exactly-Once from JDBC Transactions

Rejected because transaction commit alone cannot coordinate Source position, runtime replay, and
Sink recovery.

## Related Decisions

- ADR-004: Sink Writer/Committer Model
- ADR-009: Exactly-Once via Capability Negotiation
- ADR-011: Bounded Pull-based Single-node Runtime
- ADR-015: Strict CSV and Create-new Output
- ADR-016: JDBC Connector Contract and Type Mapping
