package io.astrasync.engine.worker;

import io.astrasync.connector.api.CheckpointContext;
import io.astrasync.connector.api.SinkCommitContext;
import io.astrasync.connector.api.data.CdcBatch;
import io.astrasync.connector.api.sink.CdcSink;
import io.astrasync.connector.api.source.CdcSource;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.CdcBatchDigests;
import io.astrasync.engine.runtime.CdcTask;
import io.astrasync.engine.runtime.CheckpointCdcWorker;
import io.astrasync.engine.runtime.CheckpointExecutionContext;
import io.astrasync.engine.runtime.CheckpointProgress;
import io.astrasync.engine.runtime.CheckpointProgressListener;
import io.astrasync.engine.runtime.CommitTokens;
import io.astrasync.engine.runtime.WorkerResult;
import java.util.Objects;
import java.util.Optional;
import java.util.function.BooleanSupplier;

/** In-process CDC worker with sink-first, source-acknowledgement checkpoint ordering. */
public final class InProcessCdcWorker implements CheckpointCdcWorker {
    private final String workerId;

    public InProcessCdcWorker(String workerId) {
        this.workerId = requireText(workerId, "workerId");
    }

    @Override
    public String workerId() {
        return workerId;
    }

    @Override
    public WorkerResult executeCdc(
            CheckpointExecutionContext context,
            CdcTask task,
            CheckpointProgressListener progressListener,
            BooleanSupplier stopRequested) {
        Objects.requireNonNull(context, "context must not be null");
        Objects.requireNonNull(task, "task must not be null");
        CheckpointProgressListener listener = CheckpointProgressListener.require(progressListener);
        BooleanSupplier stop = Objects.requireNonNull(stopRequested, "stopRequested must not be null");
        if (!context.splitId().equals(task.taskId())) {
            throw new IllegalArgumentException("CDC context and task identities do not match");
        }

        CdcSource source = task.source();
        CdcSink sink = task.sink();
        long startedNanos = System.nanoTime();
        long sequence = context.checkpointSequence();
        long readCount = 0;
        long writtenCount = 0;
        long batchCount = 0;
        int maxBatchSize = 0;
        boolean sourceOpened = false;
        boolean sinkOpened = false;
        RuntimeException failure = null;
        try {
            context.assertCurrent();
            source.openAt(context.sourcePosition());
            sourceOpened = true;
            sink.open(new CheckpointContext(
                    context.jobId(), context.executionEpoch(), task.taskId(), context::assertCurrent));
            sinkOpened = true;

            while (!stop.getAsBoolean()) {
                Optional<CdcBatch> polled = source.poll(task.pollTimeout());
                if (polled.isEmpty()) {
                    continue;
                }
                CdcBatch batch = polled.orElseThrow();
                readCount += batch.size();
                batchCount++;
                maxBatchSize = Math.max(maxBatchSize, batch.size());
                context.assertCurrent();
                long nextSequence = Math.addExact(sequence, 1);
                String batchDigest = CdcBatchDigests.sha256(batch);
                String commitToken = CommitTokens.forBatch(context.jobId(), task.taskId(), nextSequence, batchDigest);
                sink.writeBatch(
                        batch,
                        new SinkCommitContext(context.jobId(), task.taskId(), nextSequence, batchDigest, commitToken));
                if (!commitToken.equals(sink.lastCommitToken())) {
                    throw new IllegalStateException("CDC sink returned an unexpected commit token");
                }
                context.assertCurrent();
                SplitPosition position = Objects.requireNonNull(
                        source.acknowledge(batch), "CDC source returned a null checkpoint position");
                sequence = nextSequence;
                listener.onBatchCommitted(new CheckpointProgress(
                        context.jobId(),
                        context.executionEpoch(),
                        task.taskId(),
                        sequence,
                        position,
                        commitToken,
                        batchDigest));
                writtenCount += batch.size();
            }
        } catch (RuntimeException exception) {
            failure = exception;
        } finally {
            failure = close(sourceOpened, source, failure);
            failure = close(sinkOpened, sink, failure);
        }
        SyncResult metrics = new SyncResult(
                readCount, writtenCount, batchCount, maxBatchSize, Math.max(0, System.nanoTime() - startedNanos));
        if (failure != null) {
            throw new CdcTaskException(workerId, task.taskId(), failure, metrics);
        }
        return new WorkerResult(workerId, task.taskId(), metrics);
    }

    private static RuntimeException close(boolean opened, AutoCloseable resource, RuntimeException failure) {
        if (!opened) {
            return failure;
        }
        try {
            resource.close();
            return failure;
        } catch (Exception exception) {
            if (failure == null) {
                return new IllegalStateException("failed to close CDC resource", exception);
            }
            failure.addSuppressed(exception);
            return failure;
        }
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
