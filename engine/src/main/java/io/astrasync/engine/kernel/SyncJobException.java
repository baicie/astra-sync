package io.astrasync.engine.kernel;

import java.util.Objects;

public final class SyncJobException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    private final SyncStage stage;
    private final transient SyncResult partialResult;

    public SyncJobException(SyncStage stage, String message, Throwable cause, SyncResult partialResult) {
        super(message, cause);
        this.stage = Objects.requireNonNull(stage, "stage must not be null");
        this.partialResult = Objects.requireNonNull(partialResult, "partialResult must not be null");
    }

    public SyncStage stage() {
        return stage;
    }

    public SyncResult partialResult() {
        return partialResult;
    }
}
