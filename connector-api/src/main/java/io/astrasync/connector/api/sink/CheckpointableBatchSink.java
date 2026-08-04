package io.astrasync.connector.api.sink;

import io.astrasync.connector.api.CheckpointContext;

/** Optional sink contract for checkpoint progress and pre-commit epoch checks. */
public interface CheckpointableBatchSink extends BatchSink {
    void open(CheckpointContext context);

    void assertEpoch(long executionEpoch);

    String lastCommitToken();

    @Override
    default void open() {
        throw new IllegalStateException("checkpoint-aware sink requires a CheckpointContext");
    }
}
