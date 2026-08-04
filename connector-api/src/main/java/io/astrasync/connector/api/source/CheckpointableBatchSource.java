package io.astrasync.connector.api.source;

import io.astrasync.connector.api.data.RowBatch;

/** Optional source contract for durable, connector-owned resume positions. */
public interface CheckpointableBatchSource extends BatchSource {
    void openAt(SplitPosition resumePosition);

    SplitPosition positionAfter(RowBatch batch);

    @Override
    default void open() {
        openAt(SplitPosition.unbounded());
    }
}
