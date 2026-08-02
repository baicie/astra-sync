package io.astrasync.engine.kernel;

import java.util.ArrayList;
import java.util.List;
import java.util.Objects;

public final class SingleNodeSyncJob {
    private static final int DEFAULT_MAX_BATCH_RECORDS = 1_024;

    private final RecordSource source;
    private final List<RecordTransform> transforms;
    private final RecordSink sink;
    private final int maxBatchRecords;

    private SingleNodeSyncJob(Builder builder) {
        this.source = builder.source;
        this.transforms = List.copyOf(builder.transforms);
        this.sink = builder.sink;
        this.maxBatchRecords = builder.maxBatchRecords;
    }

    public static Builder builder() {
        return new Builder();
    }

    public SyncResult run() {
        long startedNanos = System.nanoTime();
        MutableMetrics metrics = new MutableMetrics(startedNanos);
        boolean sourceOpened = false;
        boolean sinkOpened = false;
        SyncJobException failure = null;

        try {
            try {
                source.open();
                sourceOpened = true;
            } catch (RuntimeException exception) {
                throw metrics.failure(SyncStage.SOURCE_OPEN, "failed to open source", exception);
            }

            try {
                sink.open();
                sinkOpened = true;
            } catch (RuntimeException exception) {
                throw metrics.failure(SyncStage.SINK_OPEN, "failed to open sink", exception);
            }

            boolean endOfInput = false;
            while (!endOfInput) {
                SyncBatch batch;
                try {
                    batch = Objects.requireNonNull(source.readBatch(maxBatchRecords), "source returned null batch");
                    if (batch.size() > maxBatchRecords) {
                        throw new IllegalStateException(
                                "source returned " + batch.size() + " records, limit is " + maxBatchRecords);
                    }
                    metrics.observeBatch(batch.size());
                } catch (RuntimeException exception) {
                    throw metrics.failure(SyncStage.SOURCE_READ, "failed to read source batch", exception);
                }

                for (SyncRecord record : batch.records()) {
                    metrics.recordRead();
                    SyncRecord transformed = record;
                    try {
                        for (RecordTransform transform : transforms) {
                            transformed =
                                    Objects.requireNonNull(transform.apply(transformed), "transform returned null");
                        }
                    } catch (RuntimeException exception) {
                        throw metrics.failure(SyncStage.TRANSFORM, "failed to transform record", exception);
                    }

                    try {
                        sink.write(transformed);
                        metrics.recordWritten();
                    } catch (RuntimeException exception) {
                        throw metrics.failure(SyncStage.SINK_WRITE, "failed to write record", exception);
                    }
                }

                metrics.batchCompleted();
                endOfInput = batch.endOfInput();
            }
        } catch (SyncJobException exception) {
            failure = exception;
        } finally {
            if (sinkOpened) {
                failure = close("sink", sink, failure, metrics);
            }
            if (sourceOpened) {
                failure = close("source", source, failure, metrics);
            }
        }

        if (failure != null) {
            throw failure;
        }
        return metrics.snapshot();
    }

    private static SyncJobException close(
            String resourceName, AutoCloseable resource, SyncJobException failure, MutableMetrics metrics) {
        try {
            resource.close();
        } catch (RuntimeException closeFailure) {
            if (failure == null) {
                return metrics.failure(SyncStage.CLOSE, "failed to close " + resourceName, closeFailure);
            }
            failure.addSuppressed(closeFailure);
        } catch (Exception closeFailure) {
            if (failure == null) {
                return metrics.failure(SyncStage.CLOSE, "failed to close " + resourceName, closeFailure);
            }
            failure.addSuppressed(closeFailure);
        }
        return failure;
    }

    public static final class Builder {
        private RecordSource source;
        private final List<RecordTransform> transforms = new ArrayList<>();
        private RecordSink sink;
        private int maxBatchRecords = DEFAULT_MAX_BATCH_RECORDS;

        public Builder source(RecordSource source) {
            this.source = Objects.requireNonNull(source, "source must not be null");
            return this;
        }

        public Builder transform(RecordTransform transform) {
            transforms.add(Objects.requireNonNull(transform, "transform must not be null"));
            return this;
        }

        public Builder sink(RecordSink sink) {
            this.sink = Objects.requireNonNull(sink, "sink must not be null");
            return this;
        }

        public Builder maxBatchRecords(int maxBatchRecords) {
            if (maxBatchRecords <= 0) {
                throw new IllegalArgumentException("maxBatchRecords must be positive");
            }
            this.maxBatchRecords = maxBatchRecords;
            return this;
        }

        public SingleNodeSyncJob build() {
            if (source == null || sink == null) {
                throw new IllegalStateException("source and sink are required");
            }
            return new SingleNodeSyncJob(this);
        }
    }

    private static final class MutableMetrics {
        private final long startedNanos;
        private long readCount;
        private long writtenCount;
        private long batchCount;
        private int maxObservedBatchSize;

        private MutableMetrics(long startedNanos) {
            this.startedNanos = startedNanos;
        }

        private void observeBatch(int size) {
            maxObservedBatchSize = Math.max(maxObservedBatchSize, size);
        }

        private void recordRead() {
            readCount++;
        }

        private void recordWritten() {
            writtenCount++;
        }

        private void batchCompleted() {
            batchCount++;
        }

        private SyncResult snapshot() {
            return new SyncResult(
                    readCount,
                    writtenCount,
                    batchCount,
                    maxObservedBatchSize,
                    Math.max(0, System.nanoTime() - startedNanos));
        }

        private SyncJobException failure(SyncStage stage, String message, Throwable cause) {
            return new SyncJobException(stage, message, cause, snapshot());
        }
    }
}
