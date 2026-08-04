package io.astrasync.engine.worker;

import io.astrasync.connector.api.CheckpointContext;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.sink.CheckpointableBatchSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.connector.api.source.CheckpointableBatchSource;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.kernel.SyncJobException;
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.kernel.SyncStage;
import io.astrasync.engine.runtime.BatchDigests;
import io.astrasync.engine.runtime.BatchExchange;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskException;
import io.astrasync.engine.runtime.BatchWorker;
import io.astrasync.engine.runtime.CheckpointBatchWorker;
import io.astrasync.engine.runtime.CheckpointExecutionContext;
import io.astrasync.engine.runtime.CheckpointProgress;
import io.astrasync.engine.runtime.CheckpointProgressListener;
import io.astrasync.engine.runtime.ExchangeFailureException;
import io.astrasync.engine.runtime.WorkerResult;
import java.util.Objects;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.atomic.AtomicInteger;

/** A Worker implementation that runs a bounded Source-to-Sink exchange in one JVM. */
public final class InProcessBatchWorker implements BatchWorker, CheckpointBatchWorker {
    private final String workerId;

    public InProcessBatchWorker(String workerId) {
        this.workerId = requireText(workerId, "workerId");
    }

    @Override
    public String workerId() {
        return workerId;
    }

    @Override
    public WorkerResult execute(BatchTask task) {
        Objects.requireNonNull(task, "task must not be null");
        long startedNanos = System.nanoTime();
        BatchExchange exchange = new BatchExchange(task.maxInFlightBatches());
        ExecutorService executor = Executors.newFixedThreadPool(2, new WorkerThreadFactory(workerId, task.taskId()));
        Future<ThreadOutcome> sourceFuture = executor.submit(() -> produce(task, exchange, startedNanos));
        Future<ThreadOutcome> sinkFuture = executor.submit(() -> consume(task, exchange, startedNanos));

        ThreadOutcome sourceOutcome = null;
        ThreadOutcome sinkOutcome = null;
        try {
            sourceOutcome = sourceFuture.get();
            sinkOutcome = sinkFuture.get();
        } catch (InterruptedException exception) {
            exchange.fail(exception);
            sourceFuture.cancel(true);
            sinkFuture.cancel(true);
            Thread.currentThread().interrupt();
            throw taskFailure(task, exception, sourceOutcome, sinkOutcome, startedNanos);
        } catch (ExecutionException exception) {
            exchange.fail(exception.getCause());
            throw taskFailure(task, exception.getCause(), sourceOutcome, sinkOutcome, startedNanos);
        } finally {
            executor.shutdownNow();
        }

        SyncJobException failure = primaryFailure(sourceOutcome, sinkOutcome);
        SyncResult metrics = aggregate(sourceOutcome.metrics(), sinkOutcome.metrics(), startedNanos);
        if (failure != null) {
            throw new BatchTaskException(workerId, task.taskId(), failure, metrics);
        }
        return new WorkerResult(workerId, task.taskId(), metrics);
    }

    @Override
    public WorkerResult executeCheckpoint(
            CheckpointExecutionContext context, BatchTask task, CheckpointProgressListener progressListener) {
        Objects.requireNonNull(context, "context must not be null");
        Objects.requireNonNull(task, "task must not be null");
        CheckpointProgressListener listener = CheckpointProgressListener.require(progressListener);
        if (!(task.source() instanceof CheckpointableBatchSource source)) {
            throw new BatchTaskException(
                    workerId,
                    task.taskId(),
                    new IllegalArgumentException("task source does not support checkpoint recovery"),
                    SyncResult.empty());
        }
        if (!(task.sink() instanceof CheckpointableBatchSink sink)) {
            throw new BatchTaskException(
                    workerId,
                    task.taskId(),
                    new IllegalArgumentException("task sink does not support checkpoint recovery"),
                    SyncResult.empty());
        }

        long startedNanos = System.nanoTime();
        long sequence = context.checkpointSequence();
        MutableMetrics metrics = new MutableMetrics();
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

            boolean endOfInput = false;
            while (!endOfInput) {
                RowBatch batch =
                        Objects.requireNonNull(source.readBatch(task.maxBatchRecords()), "source returned null batch");
                if (batch.size() > task.maxBatchRecords()) {
                    throw new IllegalStateException(
                            "source returned " + batch.size() + " records, limit is " + task.maxBatchRecords());
                }
                metrics.observe(batch);
                if (!batch.rows().isEmpty()) {
                    context.assertCurrent();
                    sink.assertEpoch(context.executionEpoch());
                    sink.writeBatch(batch);
                    context.assertCurrent();
                    sequence = Math.addExact(sequence, 1);
                    SplitPosition position = Objects.requireNonNull(
                            source.positionAfter(batch), "checkpoint source returned null position");
                    String token = Objects.requireNonNull(
                            sink.lastCommitToken(), "checkpoint sink returned null commit token");
                    listener.onBatchCommitted(new CheckpointProgress(
                            context.jobId(),
                            context.executionEpoch(),
                            task.taskId(),
                            sequence,
                            position,
                            token,
                            BatchDigests.sha256(batch)));
                    metrics.writtenCount += batch.size();
                }
                endOfInput = batch.endOfInput();
            }
        } catch (RuntimeException exception) {
            failure = exception;
        } finally {
            if (sourceOpened) {
                try {
                    source.close();
                } catch (RuntimeException closeFailure) {
                    if (failure == null) {
                        failure = closeFailure;
                    } else {
                        failure.addSuppressed(closeFailure);
                    }
                }
            }
            if (sinkOpened) {
                try {
                    sink.close();
                } catch (RuntimeException closeFailure) {
                    if (failure == null) {
                        failure = closeFailure;
                    } else {
                        failure.addSuppressed(closeFailure);
                    }
                }
            }
        }
        SyncResult result = new SyncResult(
                metrics.readCount,
                metrics.writtenCount,
                metrics.batchCount,
                metrics.maxObservedBatchSize,
                Math.max(0, System.nanoTime() - startedNanos));
        if (failure != null) {
            throw new BatchTaskException(workerId, task.taskId(), failure, result);
        }
        return new WorkerResult(workerId, task.taskId(), result);
    }

    private ThreadOutcome produce(BatchTask task, BatchExchange exchange, long startedNanos) {
        BatchSource source = task.source();
        MutableMetrics metrics = new MutableMetrics();
        SyncJobException failure = null;
        boolean directFailure = false;
        boolean opened = false;

        try {
            try {
                source.open();
                opened = true;
            } catch (RuntimeException exception) {
                failure = failure(SyncStage.SOURCE_OPEN, "failed to open source", exception, metrics, startedNanos);
                directFailure = true;
            }

            boolean endOfInput = false;
            while (failure == null && !endOfInput) {
                RowBatch batch;
                try {
                    batch = Objects.requireNonNull(
                            source.readBatch(task.maxBatchRecords()), "source returned null batch");
                    if (batch.size() > task.maxBatchRecords()) {
                        throw new IllegalStateException(
                                "source returned " + batch.size() + " records, limit is " + task.maxBatchRecords());
                    }
                    metrics.observe(batch);
                    exchange.publish(batch);
                    endOfInput = batch.endOfInput();
                } catch (RuntimeException exception) {
                    failure = failure(
                            SyncStage.SOURCE_READ, "failed to read source batch", exception, metrics, startedNanos);
                    directFailure = !(exception instanceof ExchangeFailureException);
                    exchange.fail(failure);
                }
            }
        } finally {
            if (opened) {
                CloseOutcome closeOutcome = close("source", source, failure, metrics, startedNanos);
                failure = closeOutcome.failure();
                directFailure = directFailure || closeOutcome.createdFailure();
            }
            if (failure != null) {
                exchange.fail(failure);
            }
        }
        return new ThreadOutcome(metrics.snapshot(startedNanos), failure, directFailure);
    }

    private ThreadOutcome consume(BatchTask task, BatchExchange exchange, long startedNanos) {
        BatchSink sink = task.sink();
        MutableMetrics metrics = new MutableMetrics();
        SyncJobException failure = null;
        boolean directFailure = false;
        boolean opened = false;

        try {
            try {
                sink.open();
                opened = true;
            } catch (RuntimeException exception) {
                failure = failure(SyncStage.SINK_OPEN, "failed to open sink", exception, metrics, startedNanos);
                directFailure = true;
                exchange.fail(failure);
            }

            boolean endOfInput = false;
            while (failure == null && !endOfInput) {
                try {
                    RowBatch batch = exchange.receive();
                    metrics.observe(batch);
                    if (!batch.rows().isEmpty()) {
                        sink.writeBatch(batch);
                        metrics.writtenCount += batch.size();
                    }
                    metrics.batchCount++;
                    endOfInput = batch.endOfInput();
                } catch (RuntimeException exception) {
                    failure = exception instanceof ExchangeFailureException exchangeFailure
                            ? exchangeFailureFailure(exchangeFailure, metrics, startedNanos)
                            : failure(
                                    SyncStage.SINK_WRITE,
                                    "failed to write sink batch",
                                    exception,
                                    metrics,
                                    startedNanos);
                    directFailure = !(exception instanceof ExchangeFailureException);
                    exchange.fail(failure);
                }
            }
        } finally {
            if (opened) {
                CloseOutcome closeOutcome = close("sink", sink, failure, metrics, startedNanos);
                failure = closeOutcome.failure();
                directFailure = directFailure || closeOutcome.createdFailure();
            }
            if (failure != null) {
                exchange.fail(failure);
            }
        }
        return new ThreadOutcome(metrics.snapshot(startedNanos), failure, directFailure);
    }

    private static SyncJobException primaryFailure(ThreadOutcome source, ThreadOutcome sink) {
        if (source.directFailure()) {
            return source.failure();
        }
        if (sink.directFailure()) {
            return sink.failure();
        }
        return source.failure() != null ? source.failure() : sink.failure();
    }

    private BatchTaskException taskFailure(
            BatchTask task, Throwable cause, ThreadOutcome source, ThreadOutcome sink, long startedNanos) {
        SyncResult metrics = aggregate(
                source == null ? SyncResult.empty() : source.metrics(),
                sink == null ? SyncResult.empty() : sink.metrics(),
                startedNanos);
        return new BatchTaskException(workerId, task.taskId(), cause, metrics);
    }

    private static SyncJobException exchangeFailureFailure(
            ExchangeFailureException exception, MutableMetrics metrics, long startedNanos) {
        Throwable cause = exception.getCause();
        return failure(
                SyncStage.SINK_WRITE,
                "source side of exchange failed",
                cause == null ? exception : cause,
                metrics,
                startedNanos);
    }

    private static SyncJobException failure(
            SyncStage stage, String message, Throwable cause, MutableMetrics metrics, long startedNanos) {
        return new SyncJobException(stage, message, cause, metrics.snapshot(startedNanos));
    }

    private static CloseOutcome close(
            String resourceName,
            AutoCloseable resource,
            SyncJobException existing,
            MutableMetrics metrics,
            long startedNanos) {
        try {
            resource.close();
            return new CloseOutcome(existing, false);
        } catch (Exception closeFailure) {
            if (existing == null) {
                return new CloseOutcome(
                        failure(
                                SyncStage.CLOSE,
                                "failed to close " + resourceName,
                                closeFailure,
                                metrics,
                                startedNanos),
                        true);
            }
            existing.addSuppressed(closeFailure);
            return new CloseOutcome(existing, false);
        }
    }

    private static SyncResult aggregate(SyncResult source, SyncResult sink, long startedNanos) {
        return new SyncResult(
                source.readCount(),
                sink.writtenCount(),
                source.batchCount(),
                Math.max(source.maxObservedBatchSize(), sink.maxObservedBatchSize()),
                Math.max(0, System.nanoTime() - startedNanos));
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }

    private record CloseOutcome(SyncJobException failure, boolean createdFailure) {}

    private record ThreadOutcome(SyncResult metrics, SyncJobException failure, boolean directFailure) {}

    private static final class MutableMetrics {
        private long readCount;
        private long writtenCount;
        private long batchCount;
        private int maxObservedBatchSize;

        private void observe(RowBatch batch) {
            readCount += batch.size();
            batchCount++;
            maxObservedBatchSize = Math.max(maxObservedBatchSize, batch.size());
        }

        private SyncResult snapshot(long startedNanos) {
            return new SyncResult(
                    readCount,
                    writtenCount,
                    batchCount,
                    maxObservedBatchSize,
                    Math.max(0, System.nanoTime() - startedNanos));
        }
    }

    private static final class WorkerThreadFactory implements ThreadFactory {
        private final String prefix;
        private final AtomicInteger sequence = new AtomicInteger();

        private WorkerThreadFactory(String workerId, String taskId) {
            this.prefix = "astrasync-" + workerId + "-" + taskId + "-";
        }

        @Override
        public Thread newThread(Runnable runnable) {
            Thread thread = new Thread(runnable, prefix + sequence.incrementAndGet());
            thread.setDaemon(true);
            return thread;
        }
    }
}
