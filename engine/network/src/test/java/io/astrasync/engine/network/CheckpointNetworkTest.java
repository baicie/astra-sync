package io.astrasync.engine.network;

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
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskFactory;
import io.astrasync.engine.runtime.BatchWorker;
import io.astrasync.engine.runtime.CheckpointBatchWorker;
import io.astrasync.engine.runtime.CheckpointExecutionContext;
import io.astrasync.engine.runtime.CheckpointProgress;
import io.astrasync.engine.runtime.EpochFence;
import io.astrasync.engine.runtime.WorkerResult;
import java.time.Duration;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;

class CheckpointNetworkTest {
    @Test
    void workerBlocksAtEachCommittedBatchUntilCoordinatorAcknowledgesIt() {
        List<CheckpointSource> sources = new ArrayList<>();
        CheckpointSink sink = new CheckpointSink();
        BatchTaskFactory factory = new BatchTaskFactory() {
            @Override
            public BatchTask create(SourceSplit split) {
                return new BatchTask(split, new EmptySource(), new EmptySink(), 1, 1);
            }

            @Override
            public BatchTask create(SourceSplit split, CheckpointExecutionContext context) {
                CheckpointSource source = new CheckpointSource();
                sources.add(source);
                return new BatchTask(split, source, sink, 1, 1);
            }
        };
        try (WorkerServer server =
                new WorkerServer("worker-a", 0, factory, new CheckpointWorker("worker-a"), 1, 0, 4)) {
            server.start();
            RemoteBatchWorker remote = new RemoteBatchWorker(
                    "worker-a", new WorkerClient("127.0.0.1", server.port(), Duration.ofSeconds(5)), 1);
            EpochFence fence = new EpochFence();
            fence.activate("orders", 1);
            CheckpointExecutionContext context = new CheckpointExecutionContext(
                    "orders", 1, "split-0", "fingerprint", 0, SplitPosition.unbounded(), fence);
            List<Long> sequences = new ArrayList<>();

            WorkerResult result = remote.executeCheckpoint(
                    context, task("split-0"), progress -> sequences.add(progress.checkpointSequence()));

            assertThat(result.metrics())
                    .isEqualTo(new SyncResult(2, 2, 2, 1, result.metrics().elapsedNanos()));
            assertThat(sequences).containsExactly(1L, 2L);
            assertThat(sink.values).containsExactly("1", "2");
            assertThat(sources).singleElement().satisfies(source -> assertThat(source.readCalls)
                    .isEqualTo(2));
        }
    }

    @Test
    void rejectsAnOldEpochBeforeTaskMaterialization() {
        AtomicInteger materializations = new AtomicInteger();
        BatchTaskFactory factory = new BatchTaskFactory() {
            @Override
            public BatchTask create(SourceSplit split) {
                return new BatchTask(split, new EmptySource(), new EmptySink(), 1, 1);
            }

            @Override
            public BatchTask create(SourceSplit split, CheckpointExecutionContext context) {
                materializations.incrementAndGet();
                return new BatchTask(split, new CheckpointSource(), new CheckpointSink(), 1, 1);
            }
        };
        try (WorkerServer server =
                new WorkerServer("worker-a", 0, factory, new CheckpointWorker("worker-a"), 1, 0, 4)) {
            server.start();
            RemoteBatchWorker remote = new RemoteBatchWorker(
                    "worker-a", new WorkerClient("127.0.0.1", server.port(), Duration.ofSeconds(5)), 1);

            remote.executeCheckpoint(context(1), task("split-0"), ignored -> {});
            remote.executeCheckpoint(context(2), task("split-0"), ignored -> {});

            assertThatThrownBy(() -> remote.executeCheckpoint(context(1), task("split-0"), ignored -> {}))
                    .isInstanceOf(NetworkWorkerException.class)
                    .hasMessageContaining("EPOCH_FENCED", "active epoch is 2");
            assertThat(materializations).hasValue(2);
        }
    }

    private static CheckpointExecutionContext context(long epoch) {
        EpochFence fence = new EpochFence();
        fence.activate("orders", epoch);
        return new CheckpointExecutionContext(
                "orders", epoch, "split-0", "fingerprint", 0, SplitPosition.unbounded(), fence);
    }

    private static BatchTask task(String splitId) {
        return new BatchTask(
                new SourceSplit(splitId, "source", new SplitPosition(Map.of("id", "1")), SplitPosition.unbounded()),
                new EmptySource(),
                new EmptySink(),
                1,
                1);
    }

    private static final class CheckpointSource implements CheckpointableBatchSource {
        private int index;
        private int readCalls;

        @Override
        public void openAt(SplitPosition resumePosition) {
            index = resumePosition.offsets().isEmpty()
                    ? 0
                    : Integer.parseInt(resumePosition.offsets().get("id"));
        }

        @Override
        public RowBatch readBatch(int maxRows) {
            readCalls++;
            if (index == 0) {
                index++;
                return RowBatch.data(List.of(Row.of("id", 1)));
            }
            if (index == 1) {
                index++;
                return RowBatch.last(List.of(Row.of("id", 2)));
            }
            return RowBatch.end();
        }

        @Override
        public SplitPosition positionAfter(RowBatch batch) {
            return new SplitPosition(Map.of(
                    "id", batch.rows().get(batch.rows().size() - 1).get("id").toString()));
        }

        @Override
        public void close() {}
    }

    private static final class CheckpointSink implements CheckpointableBatchSink {
        private final List<String> values = new ArrayList<>();
        private CheckpointContext context;
        private int commits;

        @Override
        public void open(CheckpointContext context) {
            this.context = context;
        }

        @Override
        public void assertEpoch(long executionEpoch) {
            assertThat(context.executionEpoch()).isEqualTo(executionEpoch);
            context.assertCurrent();
        }

        @Override
        public String lastCommitToken() {
            return "commit-" + commits;
        }

        @Override
        public void writeBatch(RowBatch batch) {
            assertEpoch(context.executionEpoch());
            values.addAll(
                    batch.rows().stream().map(row -> row.get("id").toString()).toList());
            commits++;
        }

        @Override
        public void close() {}
    }

    private static final class CheckpointWorker implements BatchWorker, CheckpointBatchWorker {
        private final String workerId;

        private CheckpointWorker(String workerId) {
            this.workerId = workerId;
        }

        @Override
        public String workerId() {
            return workerId;
        }

        @Override
        public WorkerResult execute(BatchTask task) {
            throw new UnsupportedOperationException("checkpoint test worker only");
        }

        @Override
        public WorkerResult executeCheckpoint(
                CheckpointExecutionContext context,
                BatchTask task,
                io.astrasync.engine.runtime.CheckpointProgressListener listener) {
            CheckpointableBatchSource source = (CheckpointableBatchSource) task.source();
            CheckpointableBatchSink sink = (CheckpointableBatchSink) task.sink();
            source.openAt(context.sourcePosition());
            sink.open(new CheckpointContext(
                    context.jobId(), context.executionEpoch(), task.taskId(), context::assertCurrent));
            long sequence = context.checkpointSequence();
            long read = 0;
            long written = 0;
            long batches = 0;
            int maxBatch = 0;
            try {
                while (true) {
                    RowBatch batch = source.readBatch(task.maxBatchRecords());
                    read += batch.size();
                    batches++;
                    maxBatch = Math.max(maxBatch, batch.size());
                    if (!batch.rows().isEmpty()) {
                        context.assertCurrent();
                        sink.assertEpoch(context.executionEpoch());
                        sink.writeBatch(batch);
                        sequence++;
                        listener.onBatchCommitted(new CheckpointProgress(
                                context.jobId(),
                                context.executionEpoch(),
                                task.taskId(),
                                sequence,
                                source.positionAfter(batch),
                                sink.lastCommitToken(),
                                "digest-" + sequence));
                        written += batch.size();
                    }
                    if (batch.endOfInput()) {
                        break;
                    }
                }
            } finally {
                source.close();
                sink.close();
            }
            return new WorkerResult(workerId, task.taskId(), new SyncResult(read, written, batches, maxBatch, 1));
        }
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
