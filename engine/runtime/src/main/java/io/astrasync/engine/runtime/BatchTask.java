package io.astrasync.engine.runtime;

import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.connector.api.source.SourceSplit;
import java.util.Objects;

/** A self-contained batch split assigned to one Worker. */
public record BatchTask(
        SourceSplit split,
        BatchSource source,
        BatchSink sink,
        int maxBatchRecords,
        int maxInFlightBatches,
        boolean exactlyOnce,
        AdaptiveBatchPolicy batchPolicy) {
    public BatchTask(
            SourceSplit split, BatchSource source, BatchSink sink, int maxBatchRecords, int maxInFlightBatches) {
        this(split, source, sink, maxBatchRecords, maxInFlightBatches, false);
    }

    public BatchTask(
            SourceSplit split,
            BatchSource source,
            BatchSink sink,
            int maxBatchRecords,
            int maxInFlightBatches,
            boolean exactlyOnce) {
        this(
                split,
                source,
                sink,
                maxBatchRecords,
                maxInFlightBatches,
                exactlyOnce,
                AdaptiveBatchPolicy.fixed(maxBatchRecords));
    }

    public BatchTask {
        split = Objects.requireNonNull(split, "split must not be null");
        source = Objects.requireNonNull(source, "source must not be null");
        sink = Objects.requireNonNull(sink, "sink must not be null");
        if (maxBatchRecords <= 0) {
            throw new IllegalArgumentException("maxBatchRecords must be positive");
        }
        if (maxInFlightBatches <= 0) {
            throw new IllegalArgumentException("maxInFlightBatches must be positive");
        }
        batchPolicy = Objects.requireNonNull(batchPolicy, "batchPolicy must not be null");
        if (batchPolicy.minBatchRecords() > maxBatchRecords || batchPolicy.initialBatchRecords() > maxBatchRecords) {
            throw new IllegalArgumentException("batch policy bounds must not exceed maxBatchRecords");
        }
    }

    public String taskId() {
        return split.splitId();
    }
}
