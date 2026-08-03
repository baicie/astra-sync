package io.astrasync.engine.runtime;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.engine.kernel.SyncResult;
import java.util.List;
import org.junit.jupiter.api.Test;

class RuntimeContractsTest {
    @Test
    void taskAndWorkerResultsKeepTheirIdentityAndLimits() {
        BatchSource source = new EmptySource();
        BatchSink sink = new EmptySink();
        BatchTask task = new BatchTask("split-1", source, sink, 4, 2);
        WorkerResult result = new WorkerResult("worker-1", task.taskId(), new SyncResult(1, 1, 1, 1, 0));

        assertThat(task.taskId()).isEqualTo("split-1");
        assertThat(task.source()).isSameAs(source);
        assertThat(task.sink()).isSameAs(sink);
        assertThat(task.maxBatchRecords()).isEqualTo(4);
        assertThat(task.maxInFlightBatches()).isEqualTo(2);
        assertThat(result.workerId()).isEqualTo("worker-1");
        assertThat(result.metrics().writtenCount()).isEqualTo(1);
    }

    @Test
    void taskAndResultRejectInvalidArguments() {
        assertThatThrownBy(() -> new BatchTask("", new EmptySource(), new EmptySink(), 1, 1))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new BatchTask("task", new EmptySource(), new EmptySink(), 0, 1))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new BatchTask("task", new EmptySource(), new EmptySink(), 1, 0))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new WorkerResult("", "task", SyncResult.empty()))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new WorkerResult("worker", "", SyncResult.empty()))
                .isInstanceOf(IllegalArgumentException.class);
    }

    @Test
    void enumeratorAndTaskExceptionExposeStableRuntimeBoundary() {
        BatchTask task = new BatchTask("split-1", new EmptySource(), new EmptySink(), 1, 1);
        BatchSplitEnumerator enumerator = () -> List.of(task);
        BatchTaskException exception =
                new BatchTaskException("worker-1", "split-1", new IllegalStateException("boom"), SyncResult.empty());

        assertThat(enumerator.enumerate()).containsExactly(task);
        assertThat(exception.workerId()).isEqualTo("worker-1");
        assertThat(exception.taskId()).isEqualTo("split-1");
        assertThat(exception.partialResult()).isEqualTo(SyncResult.empty());
        assertThat(exception).hasMessage("Worker 'worker-1' failed task 'split-1'");
        assertThat(exception).hasCauseInstanceOf(IllegalStateException.class);
    }

    private static final class EmptySource implements BatchSource {
        @Override
        public void open() {}

        @Override
        public RowBatch readBatch(int maxRows) {
            return RowBatch.end();
        }

        @Override
        public void close() {}
    }

    private static final class EmptySink implements BatchSink {
        @Override
        public void open() {}

        @Override
        public void writeBatch(RowBatch batch) {}

        @Override
        public void close() {}
    }
}
