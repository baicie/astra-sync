package io.astrasync.engine.coordinator;

import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitEnumerator;
import io.astrasync.engine.checkpoint.FullLoadProgress;
import io.astrasync.engine.checkpoint.SplitPlan;
import io.astrasync.engine.checkpoint.SplitProgressStore;
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.AdaptiveParallelismPolicy;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskFactory;
import io.astrasync.engine.runtime.BatchWorker;
import io.astrasync.engine.runtime.WorkerResult;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;

/** Restarts a full load by scheduling only splits without a durable successful result. */
public final class ResumableBatchCoordinator {
    private final BatchCoordinator coordinator;
    private final SplitProgressStore progressStore;

    public ResumableBatchCoordinator(List<? extends BatchWorker> workers, SplitProgressStore progressStore) {
        this.coordinator = new BatchCoordinator(workers);
        this.progressStore = Objects.requireNonNull(progressStore, "progressStore must not be null");
    }

    public ResumableRunResult run(String jobId, SplitEnumerator enumerator, BatchTaskFactory taskFactory) {
        return run(jobId, enumerator, taskFactory, null);
    }

    public ResumableRunResult run(
            String jobId,
            SplitEnumerator enumerator,
            BatchTaskFactory taskFactory,
            AdaptiveParallelismPolicy parallelismPolicy) {
        Objects.requireNonNull(enumerator, "enumerator must not be null");
        Objects.requireNonNull(taskFactory, "taskFactory must not be null");
        List<SourceSplit> splits =
                List.copyOf(Objects.requireNonNull(enumerator.enumerate(), "enumerator returned null"));
        SplitPlan plan = SplitPlan.from(splits);
        FullLoadProgress initial = progressStore.open(jobId, plan);

        List<BatchTask> pendingTasks = new ArrayList<>();
        Map<String, SourceSplit> pendingSplits = new HashMap<>();
        for (SourceSplit split : splits) {
            if (initial.completedSplits().containsKey(split.splitId())) {
                continue;
            }
            BatchTask task = Objects.requireNonNull(taskFactory.create(split), "task factory returned null");
            if (!split.equals(task.split())) {
                throw new IllegalArgumentException("task factory changed split " + split.splitId());
            }
            pendingTasks.add(task);
            pendingSplits.put(split.splitId(), split);
        }

        var completionListener = (java.util.function.Consumer<WorkerResult>) result -> progressStore.recordCompletion(
                jobId,
                plan.fingerprint(),
                Objects.requireNonNull(
                        pendingSplits.get(result.taskId()),
                        "Worker returned an unknown task result: " + result.taskId()),
                result);
        if (parallelismPolicy == null) {
            coordinator.run(pendingTasks, completionListener);
        } else {
            coordinator.runAdaptive(pendingTasks, parallelismPolicy, completionListener);
        }

        FullLoadProgress completed = progressStore.open(jobId, plan);
        if (!completed.isComplete()) {
            throw new BatchCoordinatorException("full-load run ended without completing every split", null);
        }
        List<WorkerResult> orderedResults = splits.stream()
                .map(split -> completed.completedResult(split.splitId()).orElseThrow())
                .toList();
        return new ResumableRunResult(
                orderedResults, aggregate(orderedResults), initial.completedCount(), pendingTasks.size());
    }

    private static SyncResult aggregate(List<WorkerResult> results) {
        long readCount = 0;
        long writtenCount = 0;
        long batchCount = 0;
        int maxObservedBatchSize = 0;
        long elapsedNanos = 0;
        for (WorkerResult result : results) {
            SyncResult metrics = result.metrics();
            readCount += metrics.readCount();
            writtenCount += metrics.writtenCount();
            batchCount += metrics.batchCount();
            maxObservedBatchSize = Math.max(maxObservedBatchSize, metrics.maxObservedBatchSize());
            elapsedNanos += metrics.elapsedNanos();
        }
        return new SyncResult(readCount, writtenCount, batchCount, maxObservedBatchSize, elapsedNanos);
    }
}
