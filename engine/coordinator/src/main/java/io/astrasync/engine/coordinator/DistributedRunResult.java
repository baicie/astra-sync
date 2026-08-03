package io.astrasync.engine.coordinator;

import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.WorkerResult;
import java.util.List;
import java.util.Objects;

/** Aggregate metrics and task results for one Coordinator run. */
public record DistributedRunResult(List<WorkerResult> taskResults, SyncResult metrics) {
    public DistributedRunResult {
        Objects.requireNonNull(taskResults, "taskResults must not be null");
        taskResults = List.copyOf(taskResults);
        metrics = Objects.requireNonNull(metrics, "metrics must not be null");
    }
}
