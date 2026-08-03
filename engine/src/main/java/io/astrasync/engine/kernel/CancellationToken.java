package io.astrasync.engine.kernel;

/** A synchronous signal that asks a running job to stop at its next boundary. */
@FunctionalInterface
public interface CancellationToken {
    boolean isCancelled();

    static CancellationToken neverCancelled() {
        return () -> false;
    }
}
