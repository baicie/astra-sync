package io.astrasync.engine.runtime;

/** Raised when a task attempts to continue after a newer execution epoch became active. */
public final class EpochFencedException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    public EpochFencedException(String message) {
        super(message);
    }
}
