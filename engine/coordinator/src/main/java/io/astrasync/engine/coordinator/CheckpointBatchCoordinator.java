package io.astrasync.engine.coordinator;

import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitEnumerator;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.checkpoint.CheckpointCompletion;
import io.astrasync.engine.checkpoint.CheckpointRecord;
import io.astrasync.engine.checkpoint.CheckpointStore;
import io.astrasync.engine.checkpoint.SplitPlan;
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskFactory;
import io.astrasync.engine.runtime.BatchWorker;
import io.astrasync.engine.runtime.CheckpointBatchWorker;
import io.astrasync.engine.runtime.CheckpointExecutionContext;
import io.astrasync.engine.runtime.CheckpointProgress;
import io.astrasync.engine.runtime.WorkerResult;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Objects;
import java.util.Optional;
import java.util.Set;
import java.util.concurrent.atomic.AtomicLong;

/** Executes checkpoint-aware tasks with one ordered checkpoint barrier per split. */
public final class CheckpointBatchCoordinator {
    private final List<BatchWorker> workers;
    private final CheckpointStore checkpointStore;

    public CheckpointBatchCoordinator(List<? extends BatchWorker> workers, CheckpointStore checkpointStore) {
        Objects.requireNonNull(workers, "workers must not be null");
        if (workers.isEmpty()) {
            throw new IllegalArgumentException("at least one worker is required");
        }
        List<BatchWorker> copy = new ArrayList<>(workers.size());
        Set<String> workerIds = new HashSet<>();
        for (BatchWorker worker : workers) {
            BatchWorker checked = Objects.requireNonNull(worker, "workers must not contain null");
            if (!workerIds.add(checked.workerId())) {
                throw new IllegalArgumentException("worker id is duplicated: " + checked.workerId());
            }
            copy.add(checked);
        }
        this.workers = List.copyOf(copy);
        this.checkpointStore = Objects.requireNonNull(checkpointStore, "checkpointStore must not be null");
    }

    public CheckpointRunResult run(String jobId, SplitEnumerator enumerator, BatchTaskFactory taskFactory) {
        return run(jobId, enumerator, taskFactory, 0);
    }

    public CheckpointRunResult run(
            String jobId, SplitEnumerator enumerator, BatchTaskFactory taskFactory, long executionEpoch) {
        if (executionEpoch < 0) {
            throw new IllegalArgumentException("executionEpoch must not be negative");
        }
        Objects.requireNonNull(enumerator, "enumerator must not be null");
        Objects.requireNonNull(taskFactory, "taskFactory must not be null");
        List<SourceSplit> splits =
                List.copyOf(Objects.requireNonNull(enumerator.enumerate(), "enumerator returned null"));
        Set<String> splitIds = new HashSet<>();
        for (SourceSplit split : splits) {
            if (!splitIds.add(Objects.requireNonNull(split, "enumerator returned a null split")
                    .splitId())) {
                throw new IllegalArgumentException("split id is duplicated: " + split.splitId());
            }
        }
        SplitPlan plan = SplitPlan.from(splits);
        long epoch = executionEpoch == 0
                ? checkpointStore.acquireEpoch(jobId, plan)
                : checkpointStore.acquireEpoch(jobId, plan, executionEpoch);
        List<WorkerResult> results = new ArrayList<>(splits.size());
        int resumed = 0;
        int executed = 0;
        int recovered = 0;

        for (int index = 0; index < splits.size(); index++) {
            SourceSplit split = splits.get(index);
            String splitFingerprint = plan.requireMatchingSplit(split);
            Optional<CheckpointCompletion> completion = checkpointStore.loadCompletion(jobId, split.splitId());
            if (completion.isPresent()) {
                results.add(completion.orElseThrow().workerResult());
                resumed++;
                continue;
            }
            Optional<CheckpointRecord> previous = checkpointStore.load(jobId, split.splitId());
            if (previous.isPresent()) {
                recovered++;
            }
            CheckpointExecutionContext context = new CheckpointExecutionContext(
                    jobId,
                    epoch,
                    split.splitId(),
                    splitFingerprint,
                    previous.map(CheckpointRecord::checkpointSequence).orElse(0L),
                    previous.map(CheckpointRecord::sourcePosition).orElse(SplitPosition.unbounded()),
                    new io.astrasync.engine.runtime.EpochFence());
            context.epochFence().activate(jobId, epoch);
            BatchTask task = Objects.requireNonNull(taskFactory.create(split, context), "task factory returned null");
            if (!split.equals(task.split())) {
                throw new IllegalArgumentException("task factory changed split " + split.splitId());
            }
            BatchWorker worker = workers.get(index % workers.size());
            if (!(worker instanceof CheckpointBatchWorker checkpointWorker)) {
                throw new IllegalArgumentException(
                        "Worker does not support checkpoint execution: " + worker.workerId());
            }
            AtomicLong durableSequence = new AtomicLong(context.checkpointSequence());
            WorkerResult result = checkpointWorker.executeCheckpoint(
                    context,
                    task,
                    progress -> durableSequence.set(record(jobId, epoch, split.splitId(), splitFingerprint, progress)
                            .checkpointSequence()));
            if (!split.splitId().equals(result.taskId())) {
                throw new BatchCoordinatorException(
                        "Worker returned an unexpected task result: " + result.taskId(), null);
            }
            checkpointStore.recordCompletion(new CheckpointCompletion(
                    jobId, epoch, split.splitId(), splitFingerprint, durableSequence.get(), result));
            results.add(result);
            executed++;
        }
        return new CheckpointRunResult(results, aggregate(results), epoch, resumed, executed, recovered);
    }

    private CheckpointRecord record(
            String jobId, long executionEpoch, String splitId, String splitFingerprint, CheckpointProgress progress) {
        if (!jobId.equals(progress.jobId())
                || executionEpoch != progress.executionEpoch()
                || !splitId.equals(progress.taskId())) {
            throw new BatchCoordinatorException("Worker returned an unexpected checkpoint identity", null);
        }
        CheckpointRecord record = new CheckpointRecord(
                jobId,
                progress.executionEpoch(),
                progress.taskId(),
                splitFingerprint,
                progress.checkpointSequence(),
                progress.sourcePosition(),
                progress.sinkCommitToken(),
                progress.batchDigest());
        CheckpointRecord durable = checkpointStore.record(record);
        if (!durable.equals(record)) {
            throw new BatchCoordinatorException(
                    "checkpoint store returned a different record for " + progress.taskId(), null);
        }
        return durable;
    }

    private static SyncResult aggregate(List<WorkerResult> results) {
        long read = 0;
        long written = 0;
        long batches = 0;
        int maxBatch = 0;
        long elapsed = 0;
        for (WorkerResult result : results) {
            SyncResult metrics = result.metrics();
            read += metrics.readCount();
            written += metrics.writtenCount();
            batches += metrics.batchCount();
            maxBatch = Math.max(maxBatch, metrics.maxObservedBatchSize());
            elapsed += metrics.elapsedNanos();
        }
        return new SyncResult(read, written, batches, maxBatch, elapsed);
    }
}
