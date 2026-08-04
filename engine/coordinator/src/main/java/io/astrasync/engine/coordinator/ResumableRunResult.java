package io.astrasync.engine.coordinator;

import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.WorkerResult;
import java.util.List;
import java.util.Objects;

/** Cumulative split results and invocation counts after a resumable full-load run completes. */
public record ResumableRunResult(
        List<WorkerResult> taskResults, SyncResult metrics, int resumedSplitCount, int executedSplitCount) {
    public ResumableRunResult {
        Objects.requireNonNull(taskResults, "taskResults must not be null");
        taskResults = List.copyOf(taskResults);
        metrics = Objects.requireNonNull(metrics, "metrics must not be null");
        if (resumedSplitCount < 0 || executedSplitCount < 0) {
            throw new IllegalArgumentException("split counts must not be negative");
        }
        if (resumedSplitCount + executedSplitCount != taskResults.size()) {
            throw new IllegalArgumentException("split counts must equal the completed task result count");
        }
    }
}
