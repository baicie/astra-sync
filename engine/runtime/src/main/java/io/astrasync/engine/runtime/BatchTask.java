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
        boolean exactlyOnce) {
    public BatchTask(
            SourceSplit split, BatchSource source, BatchSink sink, int maxBatchRecords, int maxInFlightBatches) {
        this(split, source, sink, maxBatchRecords, maxInFlightBatches, false);
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
    }

    public String taskId() {
        return split.splitId();
    }
}
