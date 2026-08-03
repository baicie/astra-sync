package io.astrasync.engine.runtime;

import io.astrasync.engine.kernel.SyncResult;
import java.util.Objects;

/** A structured failure for one split execution. */
public final class BatchTaskException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    private final String workerId;
    private final String taskId;
    private final SyncResult partialResult;

    public BatchTaskException(String workerId, String taskId, Throwable cause, SyncResult partialResult) {
        super("Worker '" + workerId + "' failed task '" + taskId + "'", cause);
        this.workerId = requireText(workerId, "workerId");
        this.taskId = requireText(taskId, "taskId");
        this.partialResult = Objects.requireNonNull(partialResult, "partialResult must not be null");
    }

    public String workerId() {
        return workerId;
    }

    public String taskId() {
        return taskId;
    }

    public SyncResult partialResult() {
        return partialResult;
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
