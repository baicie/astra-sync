package io.astrasync.engine.runtime;

import java.util.Objects;

/** Coordinator callback that durably acknowledges one committed batch. */
@FunctionalInterface
public interface CheckpointProgressListener {
    void onBatchCommitted(CheckpointProgress progress);

    static CheckpointProgressListener require(CheckpointProgressListener listener) {
        return Objects.requireNonNull(listener, "listener must not be null");
    }
}
