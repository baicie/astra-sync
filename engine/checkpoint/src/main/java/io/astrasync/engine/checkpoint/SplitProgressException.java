package io.astrasync.engine.checkpoint;

/** Signals durable split-progress I/O or format failure. */
public final class SplitProgressException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    public SplitProgressException(String message, Throwable cause) {
        super(message, cause);
    }
}
