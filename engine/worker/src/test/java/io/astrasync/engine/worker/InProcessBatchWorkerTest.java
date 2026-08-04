package io.astrasync.engine.worker;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.CheckpointContext;
import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.sink.CheckpointableBatchSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.connector.api.source.CheckpointableBatchSource;
import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.kernel.SyncStage;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskException;
import io.astrasync.engine.runtime.CheckpointExecutionContext;
import io.astrasync.engine.runtime.EpochFence;
import io.astrasync.engine.runtime.EpochFencedException;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

class InProcessBatchWorkerTest {
    @Test
    void runsSourceAndSinkThroughBoundedExchangeAndClosesBoth() {
        List<Row> written = new ArrayList<>();
        LifecycleSource source =
                new LifecycleSource(RowBatch.data(List.of(Row.of("id", 1))), RowBatch.last(List.of(Row.of("id", 2))));
        LifecycleSink sink = new LifecycleSink(written);

        var result = new InProcessBatchWorker("worker-a").execute(new BatchTask(split("split-1"), source, sink, 2, 1));

        assertThat(written).extracting(row -> row.get("id")).containsExactly(1, 2);
        assertThat(result.metrics().readCount()).isEqualTo(2);
        assertThat(result.metrics().writtenCount()).isEqualTo(2);
        assertThat(result.metrics().batchCount()).isEqualTo(2);
        assertThat(result.metrics().maxObservedBatchSize()).isEqualTo(1);
        assertThat(source.openCount).isEqualTo(1);
        assertThat(source.closeCount).isEqualTo(1);
        assertThat(sink.openCount).isEqualTo(1);
        assertThat(sink.closeCount).isEqualTo(1);
    }

    @Test
    void sinkFailureStopsSourceAndReportsStructuredPartialMetrics() {
        LifecycleSource source =
                new LifecycleSource(RowBatch.data(List.of(Row.of("id", 1))), RowBatch.last(List.of(Row.of("id", 2))));
        LifecycleSink sink = new LifecycleSink(new ArrayList<>());
        sink.writeFailure = new IllegalStateException("sink failed");

        assertThatThrownBy(() -> new InProcessBatchWorker("worker-a")
                        .execute(new BatchTask(split("split-1"), source, sink, 1, 1)))
                .isInstanceOfSatisfying(BatchTaskException.class, exception -> {
                    assertThat(exception.taskId()).isEqualTo("split-1");
                    assertThat(exception.workerId()).isEqualTo("worker-a");
                    assertThat(exception.getCause())
                            .isInstanceOfSatisfying(
                                    io.astrasync.engine.kernel.SyncJobException.class,
                                    failure -> assertThat(failure.stage()).isEqualTo(SyncStage.SINK_WRITE));
                    assertThat(exception.partialResult().readCount()).isGreaterThanOrEqualTo(1);
                });
        assertThat(source.closeCount).isEqualTo(1);
        assertThat(sink.closeCount).isEqualTo(1);
    }

    @Test
    void fencesAnOldEpochBeforeTheSinkCommit() {
        EpochFence fence = new EpochFence();
        fence.activate("orders", 1);
        FencingSource source = new FencingSource(fence);
        CheckpointSink sink = new CheckpointSink();
        CheckpointExecutionContext context = new CheckpointExecutionContext(
                "orders", 1, "split-1", "fingerprint", 0, SplitPosition.unbounded(), fence);

        assertThatThrownBy(() -> new InProcessBatchWorker("worker-a")
                        .executeCheckpoint(context, new BatchTask(split("split-1"), source, sink, 1, 1), ignored -> {}))
                .isInstanceOf(BatchTaskException.class)
                .hasRootCauseInstanceOf(EpochFencedException.class);
        assertThat(sink.writeCount).isZero();
    }

    private static SourceSplit split(String splitId) {
        return new SourceSplit(splitId, "test-source", new SplitPosition(Map.of("id", "1")), SplitPosition.unbounded());
    }

    private static final class LifecycleSource implements BatchSource {
        private final List<RowBatch> batches;
        private int index;
        private int openCount;
        private int closeCount;

        private LifecycleSource(RowBatch... batches) {
            this.batches = List.of(batches);
        }

        @Override
        public void open() {
            openCount++;
        }

        @Override
        public RowBatch readBatch(int maxRows) {
            return batches.get(index++);
        }

        @Override
        public void close() {
            closeCount++;
        }
    }

    private static final class LifecycleSink implements BatchSink {
        private final List<Row> written;
        private RuntimeException writeFailure;
        private int openCount;
        private int closeCount;

        private LifecycleSink(List<Row> written) {
            this.written = written;
        }

        @Override
        public void open() {
            openCount++;
        }

        @Override
        public void writeBatch(RowBatch batch) {
            if (writeFailure != null) {
                throw writeFailure;
            }
            written.addAll(batch.rows());
        }

        @Override
        public void close() {
            closeCount++;
        }
    }

    private static final class FencingSource implements CheckpointableBatchSource {
        private final EpochFence fence;

        private FencingSource(EpochFence fence) {
            this.fence = fence;
        }

        @Override
        public void openAt(SplitPosition resumePosition) {}

        @Override
        public RowBatch readBatch(int maxRows) {
            fence.activate("orders", 2);
            return RowBatch.last(List.of(Row.of("id", 1)));
        }

        @Override
        public SplitPosition positionAfter(RowBatch batch) {
            return new SplitPosition(Map.of("id", "1"));
        }

        @Override
        public void close() {}
    }

    private static final class CheckpointSink implements CheckpointableBatchSink {
        private CheckpointContext context;
        private int writeCount;

        @Override
        public void open(CheckpointContext context) {
            this.context = context;
        }

        @Override
        public void assertEpoch(long executionEpoch) {
            context.assertCurrent();
        }

        @Override
        public String lastCommitToken() {
            return "commit-1";
        }

        @Override
        public void writeBatch(RowBatch batch) {
            writeCount++;
        }

        @Override
        public void close() {}
    }
}
