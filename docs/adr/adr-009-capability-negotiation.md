# ADR-009: Exactly-Once via Capability Negotiation

## Status

Accepted

## Context

"Exactly-once" is often marketed as a configuration switch, but its actual availability depends on:
- Source: Can it replay from an offset?
- Channel: Is data persisted durably?
- Sink: Can it commit atomically or idempotently?

A misconfigured "exactly-once" with an incapable sink results in either:
- Silent data loss (at-most-once)
- Duplicate data (at-least-once without idempotency)
- Runtime failures

## Decision

Implement capability-based delivery guarantee negotiation:

### Capability Model

```
Source Capabilities:
  - replayableOffsets: true/false
  - consistentSnapshot: true/false
  - transactionBoundary: [NONE, STATEMENT, TRANSACTION]

Sink Capabilities:
  - transactionalCommit: true/false
  - idempotentWrite: true/false
  - uniqueKey: [PRIMARY_KEY, NATURAL_KEY, NONE]
```

### Negotiation Matrix

| Source | Sink | Result |
|--------|------|--------|
| replayableOffsets + consistentSnapshot | transactionalCommit | Exactly-Once |
| replayableOffsets | idempotentWrite + uniqueKey | At-Least-Once (dedup) |
| replayableOffsets | none | At-Least-Once |
| replayableOffsets | none | At-Most-Once |
| none | any | At-Most-Once |

### Validation at Job Creation

1. Parse requested delivery guarantee
2. Query source capabilities
3. Query sink capabilities
4. Calculate achievable guarantee
5. If achievable < requested:
   - Reject job with clear explanation
   - Suggest compatible configuration

## Consequences

### Positive
- Honest communication of guarantees
- Prevents silent data integrity issues
- Clear failure messages guide configuration
- Users understand actual semantics

### Negative
- More complex configuration validation
- Users may be disappointed by capability limitations
- Additional metadata needed in connectors

## References

- [Flink Exactly-Once](https://nightlies.apache.org/flink/flink-docs-stable/docs/concepts/stateful-stream-processing/)
- [SeaTunnel Delivery Guarantees](https://seatunnel.apache.org/docs/concept/delivery)
