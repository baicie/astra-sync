package io.astrasync.engine.coordinator;

import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.WorkerResult;
import java.util.List;
import java.util.Objects;

/** Cumulative result for one checkpoint-aware Coordinator invocation. */
public record CheckpointRunResult(
        List<WorkerResult> taskResults,
        SyncResult metrics,
        long executionEpoch,
        int resumedSplitCount,
        int executedSplitCount,
        int recoveredSplitCount) {
    public CheckpointRunResult {
        taskResults = List.copyOf(Objects.requireNonNull(taskResults, "taskResults must not be null"));
        metrics = Objects.requireNonNull(metrics, "metrics must not be null");
        if (executionEpoch <= 0
                || resumedSplitCount < 0
                || executedSplitCount < 0
                || recoveredSplitCount < 0
                || recoveredSplitCount > executedSplitCount) {
            throw new IllegalArgumentException("checkpoint result values are invalid");
        }
        if (resumedSplitCount + executedSplitCount != taskResults.size()) {
            throw new IllegalArgumentException("split counts must equal the completed task result count");
        }
    }
}
