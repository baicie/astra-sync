package io.astrasync.engine.coordinator;

/** Signals Coordinator lifecycle failure outside a task's own execution error. */
public final class BatchCoordinatorException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    public BatchCoordinatorException(String message, Throwable cause) {
        super(message, cause);
    }
}
