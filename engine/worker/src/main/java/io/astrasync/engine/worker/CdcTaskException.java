package io.astrasync.engine.worker;

import io.astrasync.engine.kernel.SyncResult;
import java.util.Objects;

/** CDC task failure with partial processing metrics. */
public final class CdcTaskException extends RuntimeException {
    private static final long serialVersionUID = 1L;

    private final String workerId;
    private final String taskId;
    private final SyncResult metrics;

    CdcTaskException(String workerId, String taskId, Throwable cause, SyncResult metrics) {
        super("CDC task '" + taskId + "' failed on worker '" + workerId + "'", cause);
        this.workerId = workerId;
        this.taskId = taskId;
        this.metrics = Objects.requireNonNull(metrics, "metrics must not be null");
    }

    public String workerId() {
        return workerId;
    }

    public String taskId() {
        return taskId;
    }

    public SyncResult metrics() {
        return metrics;
    }
}
