package io.astrasync.engine.checkpoint;

/** Base failure for durable checkpoint storage. */
public class CheckpointStoreException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    public CheckpointStoreException(String message, Throwable cause) {
        super(message, cause);
    }

    public CheckpointStoreException(String message) {
        super(message);
    }
}
