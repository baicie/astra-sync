# ADR-008: Dual Format Support (Row and Arrow)

## Status

Accepted

## Context

Different data processing scenarios have different optimal formats:
- Row format: Better for point updates, random access, CDC
- Arrow format: Better for bulk scans, aggregations, vectorization

Forcing a single format would force a compromise that hurts either:
- CDC latency (waiting for batch accumulation)
- Batch throughput (processing row-by-row)

## Decision

Support both Row Binary and Arrow RecordBatch internally:

### Row Binary Format

**Use for:**
- CDC events (low latency)
- Point lookups
- Update-heavy workloads
- Small batches

**Encoding:**
```
[Field1][Field2][Field3]...  (fixed or variable length)
```

### Arrow RecordBatch

**Use for:**
- Bulk data migration
- Analytical queries
- Large batch processing
- Worker-to-worker shuffle

**Encoding:**
```
Schema + Column Arrays (column-oriented)
```

### Selection Logic

| Scenario | Format | Rationale |
|----------|--------|------------|
| CDC Poll | Row | Low latency requirement |
| Batch Read | Arrow | Bulk transfer efficiency |
| Shuffle | Arrow | Network efficiency |
| Small Batch (<100 rows) | Row | Overhead not justified |
| Aggregation | Arrow | SIMD vectorization |

## Consequences

### Positive
- Optimal format for each use case
- Reduced serialization overhead
- Better CPU utilization via Arrow
- Lower latency for CDC workloads

### Negative
- Format conversion overhead in mixed scenarios
- More complex memory management
- Connector complexity (must support both)

## References

- [Apache Arrow Format](https://arrow.apache.org/docs/format/Columnar.html)
- [Arrow Memory Management](https://arrow.apache.org/docs/memory.html)
