# ADR-002: Direct Pipeline Mode as Default

## Status

Accepted

## Context

Many data synchronization scenarios (database-to-database, database-to-warehouse, CDC) do not require the complexity of a message queue. Forcing all data through Kafka or Pulsar introduces:
- Additional deployment infrastructure
- Disk I/O for message persistence
- Serialization/deserialization overhead
- Operational complexity for MQ cluster management
- Added latency from producer buffering and consumer polling

## Decision

Default to Direct Pipeline Mode where data flows directly from Source Worker to Sink Worker:

```
Source Worker → Network Exchange → Sink Worker
```

MQ (Durable Relay Mode) is available as an optional deployment pattern when:
- Multiple independent downstream consumers are needed
- Cross-region synchronization is required
- Long-term event replay is needed
- Downstream systems have frequent downtime

Batch Materialization Mode is used for:
- TB/PB-scale data migration
- Cross-cloud data transfer
- Bandwidth-constrained environments

## Consequences

### Positive
- Lowest possible latency for most use cases
- Simpler deployment in single-region scenarios
- Reduced infrastructure costs
- Faster time-to-value for new users

### Negative
- No built-in message replay for failed downstream consumers
- Source and sink must be online simultaneously
- Less suitable for complex fan-out scenarios

## References

- [Kafka vs Direct Transfer](https://developer.confluent.io/learn/kafka-multi-datacenter/)
- [SeaTunnel Deployment Modes](https://seatunnel.apache.org/docs/concept/deployment)
