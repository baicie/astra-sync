package io.astrasync.engine.coordinator;

import static org.assertj.core.api.Assertions.assertThat;

import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.checkpoint.CheckpointCompletion;
import io.astrasync.engine.checkpoint.CheckpointRecord;
import io.astrasync.engine.checkpoint.CheckpointStore;
import io.astrasync.engine.checkpoint.SplitPlan;
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.observability.DataPlaneMetrics;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskFactory;
import io.astrasync.engine.runtime.BatchWorker;
import io.astrasync.engine.runtime.CheckpointBatchWorker;
import io.astrasync.engine.runtime.CheckpointExecutionContext;
import io.astrasync.engine.runtime.CheckpointProgress;
import io.astrasync.engine.runtime.CheckpointProgressListener;
import io.astrasync.engine.runtime.WorkerResult;
import io.micrometer.core.instrument.simple.SimpleMeterRegistry;
import java.util.Optional;
import org.junit.jupiter.api.Test;

class CheckpointBatchCoordinatorMetricsTest {
    private static final String JOB_ID = "1f36d9c6-77a2-4e83-8c7d-dc59b9b94a52";

    @Test
    void recordsBatchAndCheckpointObservationsForCheckpointExecution() {
        SimpleMeterRegistry registry = new SimpleMeterRegistry();
        CheckpointBatchCoordinator coordinator = new CheckpointBatchCoordinator(
                java.util.List.of(new RecordingWorker()),
                new InMemoryCheckpointStore(),
                new DataPlaneMetrics(registry));
        SourceSplit split = new SourceSplit("split-1", "source", SplitPosition.unbounded(), SplitPosition.unbounded());
        BatchTaskFactory factory = ignored -> new BatchTask(split, new EmptySource(), new EmptySink(), 25, 1);

        coordinator.run(JOB_ID, () -> java.util.List.of(split), factory);

        assertThat(registry.get("coordinator.batch.size")
                        .tag("job_id", JOB_ID)
                        .summary()
                        .count())
                .isEqualTo(1);
        assertThat(registry.get("coordinator.batch.duration")
                        .tag("job_id", JOB_ID)
                        .timer()
                        .count())
                .isEqualTo(1);
        assertThat(registry.get("coordinator.checkpoint.duration")
                        .tag("job_id", JOB_ID)
                        .tag("outcome", "success")
                        .timer()
                        .count())
                .isEqualTo(1);
    }

    private static final class RecordingWorker implements BatchWorker, CheckpointBatchWorker {
        @Override
        public String workerId() {
            return "worker-a";
        }

        @Override
        public WorkerResult execute(BatchTask task) {
            throw new UnsupportedOperationException("checkpoint execution is required");
        }

        @Override
        public WorkerResult executeCheckpoint(
                CheckpointExecutionContext context, BatchTask task, CheckpointProgressListener progressListener) {
            progressListener.onBatchCommitted(new CheckpointProgress(
                    context.jobId(),
                    context.executionEpoch(),
                    task.taskId(),
                    1,
                    SplitPosition.unbounded(),
                    "commit-1",
                    "digest-1"));
            return new WorkerResult(workerId(), task.taskId(), new SyncResult(2, 2, 1, 2, 1));
        }
    }

    private static final class InMemoryCheckpointStore implements CheckpointStore {
        private CheckpointRecord record;

        @Override
        public long acquireEpoch(String jobId, SplitPlan plan) {
            return 1;
        }

        @Override
        public long acquireEpoch(String jobId, SplitPlan plan, long executionEpoch) {
            return executionEpoch;
        }

        @Override
        public Optional<CheckpointRecord> load(String jobId, String splitId) {
            return Optional.ofNullable(record);
        }

        @Override
        public Optional<CheckpointCompletion> loadCompletion(String jobId, String splitId) {
            return Optional.empty();
        }

        @Override
        public CheckpointRecord record(CheckpointRecord checkpoint) {
            record = checkpoint;
            return checkpoint;
        }

        @Override
        public CheckpointCompletion recordCompletion(CheckpointCompletion completion) {
            return completion;
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
