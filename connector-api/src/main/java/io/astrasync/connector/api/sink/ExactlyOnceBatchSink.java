package io.astrasync.connector.api.sink;

import io.astrasync.connector.api.SinkCommitContext;
import io.astrasync.connector.api.data.RowBatch;

/** Sink contract that makes a repeated logical batch commit a retry-safe no-op. */
public interface ExactlyOnceBatchSink extends CheckpointableBatchSink {
    void writeBatch(RowBatch batch, SinkCommitContext commitContext);

    @Override
    default void writeBatch(RowBatch batch) {
        throw new IllegalStateException("exactly-once sink requires a SinkCommitContext");
    }
}
