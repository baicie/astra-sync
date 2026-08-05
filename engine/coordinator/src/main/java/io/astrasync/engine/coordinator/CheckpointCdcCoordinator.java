package io.astrasync.engine.coordinator;

import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.checkpoint.CheckpointRecord;
import io.astrasync.engine.checkpoint.CheckpointStore;
import io.astrasync.engine.checkpoint.SplitPlan;
import io.astrasync.engine.runtime.CdcTask;
import io.astrasync.engine.runtime.CheckpointCdcWorker;
import io.astrasync.engine.runtime.CheckpointExecutionContext;
import io.astrasync.engine.runtime.CheckpointProgress;
import io.astrasync.engine.runtime.EpochFence;
import io.astrasync.engine.runtime.WorkerResult;
import java.util.List;
import java.util.Objects;
import java.util.Optional;
import java.util.concurrent.atomic.AtomicLong;
import java.util.function.BooleanSupplier;

/** Owns epochs and durable source progress for one unbounded CDC split. */
public final class CheckpointCdcCoordinator {
    private final CheckpointCdcWorker worker;
    private final CheckpointStore checkpointStore;

    public CheckpointCdcCoordinator(CheckpointCdcWorker worker, CheckpointStore checkpointStore) {
        this.worker = Objects.requireNonNull(worker, "worker must not be null");
        this.checkpointStore = Objects.requireNonNull(checkpointStore, "checkpointStore must not be null");
    }

    public CdcRunResult run(String jobId, String sourceId, CdcTask task, BooleanSupplier stopRequested) {
        return run(jobId, sourceId, task, stopRequested, 0);
    }

    public CdcRunResult run(
            String jobId, String sourceId, CdcTask task, BooleanSupplier stopRequested, long maxCheckpoints) {
        Objects.requireNonNull(task, "task must not be null");
        if (maxCheckpoints < 0) {
            throw new IllegalArgumentException("maxCheckpoints must not be negative");
        }
        SourceSplit split = new SourceSplit(
                task.taskId(), requireText(sourceId, "sourceId"), SplitPosition.unbounded(), SplitPosition.unbounded());
        SplitPlan plan = SplitPlan.from(List.of(split));
        long epoch = checkpointStore.acquireEpoch(requireText(jobId, "jobId"), plan);
        Optional<CheckpointRecord> previous = checkpointStore.load(jobId, task.taskId());
        String splitFingerprint = plan.requireMatchingSplit(split);
        EpochFence fence = new EpochFence();
        fence.activate(jobId, epoch);
        CheckpointExecutionContext context = new CheckpointExecutionContext(
                jobId,
                epoch,
                task.taskId(),
                splitFingerprint,
                previous.map(CheckpointRecord::checkpointSequence).orElse(0L),
                previous.map(CheckpointRecord::sourcePosition).orElse(SplitPosition.unbounded()),
                fence);
        AtomicLong durableSequence = new AtomicLong(context.checkpointSequence());
        long initialSequence = context.checkpointSequence();
        BooleanSupplier requestedStop = Objects.requireNonNull(stopRequested, "stopRequested must not be null");
        WorkerResult result = worker.executeCdc(
                context,
                task,
                progress -> durableSequence.set(record(context, progress).checkpointSequence()),
                () -> requestedStop.getAsBoolean()
                        || maxCheckpoints > 0 && durableSequence.get() - initialSequence >= maxCheckpoints);
        return new CdcRunResult(result, epoch, durableSequence.get(), previous.isPresent());
    }

    private CheckpointRecord record(CheckpointExecutionContext context, CheckpointProgress progress) {
        if (!context.jobId().equals(progress.jobId())
                || context.executionEpoch() != progress.executionEpoch()
                || !context.splitId().equals(progress.taskId())) {
            throw new BatchCoordinatorException("CDC Worker returned an unexpected checkpoint identity", null);
        }
        CheckpointRecord record = new CheckpointRecord(
                context.jobId(),
                context.executionEpoch(),
                context.splitId(),
                context.splitFingerprint(),
                progress.checkpointSequence(),
                progress.sourcePosition(),
                progress.sinkCommitToken(),
                progress.batchDigest());
        CheckpointRecord durable = checkpointStore.record(record);
        if (!record.equals(durable)) {
            throw new BatchCoordinatorException("checkpoint store returned a different CDC record", null);
        }
        return durable;
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
