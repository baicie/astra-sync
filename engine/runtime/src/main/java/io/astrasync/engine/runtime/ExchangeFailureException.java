package io.astrasync.engine.runtime;

/** Signals that the other side of a bounded exchange failed. */
public final class ExchangeFailureException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    public ExchangeFailureException(String message, Throwable cause) {
        super(message, cause);
    }
}
