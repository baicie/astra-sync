package io.astrasync.engine.network;

import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskFactory;
import java.util.Objects;

/** Creates descriptor-only tasks for a Coordinator whose real resources live on a remote Worker. */
public final class RemoteTaskFactory implements BatchTaskFactory {
    private final int maxBatchRecords;
    private final int maxInFlightBatches;
    private final boolean exactlyOnce;

    public RemoteTaskFactory(int maxBatchRecords, int maxInFlightBatches) {
        this(maxBatchRecords, maxInFlightBatches, false);
    }

    public RemoteTaskFactory(int maxBatchRecords, int maxInFlightBatches, boolean exactlyOnce) {
        if (maxBatchRecords <= 0) {
            throw new IllegalArgumentException("maxBatchRecords must be positive");
        }
        if (maxInFlightBatches <= 0) {
            throw new IllegalArgumentException("maxInFlightBatches must be positive");
        }
        this.maxBatchRecords = maxBatchRecords;
        this.maxInFlightBatches = maxInFlightBatches;
        this.exactlyOnce = exactlyOnce;
    }

    @Override
    public BatchTask create(SourceSplit split) {
        SourceSplit checked = Objects.requireNonNull(split, "split must not be null");
        return new BatchTask(
                checked,
                new DescriptorSource(),
                new DescriptorSink(),
                maxBatchRecords,
                maxInFlightBatches,
                exactlyOnce);
    }

    private static final class DescriptorSource implements BatchSource {
        @Override
        public void open() {
            throw new IllegalStateException("remote descriptor source must not be opened on the Coordinator");
        }

        @Override
        public RowBatch readBatch(int maxRows) {
            throw new IllegalStateException("remote descriptor source must not be read on the Coordinator");
        }

        @Override
        public void close() {}
    }

    private static final class DescriptorSink implements BatchSink {
        @Override
        public void open() {
            throw new IllegalStateException("remote descriptor sink must not be opened on the Coordinator");
        }

        @Override
        public void writeBatch(RowBatch batch) {
            throw new IllegalStateException("remote descriptor sink must not be written on the Coordinator");
        }

        @Override
        public void close() {}
    }
}
