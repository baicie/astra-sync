package io.astrasync.engine.runtime;

import io.astrasync.engine.kernel.SyncResult;
import java.util.Objects;

/** Metrics returned after one Worker completes one split. */
public record WorkerResult(String workerId, String taskId, SyncResult metrics) {
    public WorkerResult {
        workerId = requireText(workerId, "workerId");
        taskId = requireText(taskId, "taskId");
        metrics = Objects.requireNonNull(metrics, "metrics must not be null");
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
