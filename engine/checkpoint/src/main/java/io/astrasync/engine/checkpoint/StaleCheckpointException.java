package io.astrasync.engine.checkpoint;

/** Raised when an old epoch or out-of-order checkpoint attempts to advance durable state. */
public final class StaleCheckpointException extends CheckpointStoreException {
    private static final long serialVersionUID = 1L;

    public StaleCheckpointException(String message) {
        super(message);
    }
}
