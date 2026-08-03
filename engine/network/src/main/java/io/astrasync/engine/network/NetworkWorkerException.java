package io.astrasync.engine.network;

/** A transport or protocol failure raised by a remote Worker boundary. */
public final class NetworkWorkerException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    public NetworkWorkerException(String message) {
        super(message);
    }

    public NetworkWorkerException(String message, Throwable cause) {
        super(message, cause);
    }
}
