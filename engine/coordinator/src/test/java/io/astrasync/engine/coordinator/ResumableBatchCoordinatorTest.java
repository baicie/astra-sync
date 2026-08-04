package io.astrasync.engine.coordinator;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.checkpoint.FileSplitProgressStore;
import io.astrasync.engine.checkpoint.SplitPlanMismatchException;
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchWorker;
import io.astrasync.engine.runtime.WorkerResult;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class ResumableBatchCoordinatorTest {
    @TempDir
    Path progressDirectory;

    @Test
    void restartSkipsDurablyCompletedSplitsAndFinishesRemainingWork() {
        List<SourceSplit> splits = List.of(split("split-1", "1"), split("split-2", "2"), split("split-3", "3"));
        List<String> firstAssignments = new CopyOnWriteArrayList<>();
        FileSplitProgressStore store = new FileSplitProgressStore(progressDirectory);
        ResumableBatchCoordinator firstAttempt =
                new ResumableBatchCoordinator(List.of(worker(firstAssignments, "split-2")), store);

        assertThatThrownBy(() -> firstAttempt.run("orders-load", () -> splits, ResumableBatchCoordinatorTest::task))
                .isInstanceOf(IllegalStateException.class)
                .hasMessage("planned task failure: split-2");
        assertThat(firstAssignments).containsExactly("split-1", "split-2");
        assertThat(store.load("orders-load").orElseThrow().completedSplits()).containsOnlyKeys("split-1");

        List<String> resumedAssignments = new CopyOnWriteArrayList<>();
        List<String> materialized = new CopyOnWriteArrayList<>();
        ResumableRunResult resumed = new ResumableBatchCoordinator(
                        List.of(worker(resumedAssignments, null)), new FileSplitProgressStore(progressDirectory))
                .run("orders-load", () -> splits, split -> {
                    materialized.add(split.splitId());
                    return task(split);
                });

        assertThat(materialized).containsExactly("split-2", "split-3");
        assertThat(resumedAssignments).containsExactly("split-2", "split-3");
        assertThat(resumed.taskResults())
                .extracting(WorkerResult::taskId)
                .containsExactly("split-1", "split-2", "split-3");
        assertThat(resumed.resumedSplitCount()).isEqualTo(1);
        assertThat(resumed.executedSplitCount()).isEqualTo(2);
        assertThat(resumed.executionEpoch()).isZero();
        assertThat(resumed.recoveredSplitCount()).isZero();
        assertThat(resumed.metrics().readCount()).isEqualTo(3);
        assertThat(resumed.metrics().writtenCount()).isEqualTo(3);

        List<String> unexpectedMaterialization = new CopyOnWriteArrayList<>();
        ResumableRunResult alreadyComplete = new ResumableBatchCoordinator(
                        List.of(worker(new CopyOnWriteArrayList<>(), null)), store)
                .run("orders-load", () -> splits, split -> {
                    unexpectedMaterialization.add(split.splitId());
                    return task(split);
                });
        assertThat(unexpectedMaterialization).isEmpty();
        assertThat(alreadyComplete.resumedSplitCount()).isEqualTo(3);
        assertThat(alreadyComplete.executedSplitCount()).isZero();
    }

    @Test
    void rejectsSplitPlanDriftBeforeMaterializingNewTasks() {
        FileSplitProgressStore store = new FileSplitProgressStore(progressDirectory);
        SourceSplit original = split("split-1", "1");
        new ResumableBatchCoordinator(List.of(worker(new CopyOnWriteArrayList<>(), null)), store)
                .run("orders-load", () -> List.of(original), ResumableBatchCoordinatorTest::task);
        List<String> materialized = new CopyOnWriteArrayList<>();

        assertThatThrownBy(
                        () -> new ResumableBatchCoordinator(List.of(worker(new CopyOnWriteArrayList<>(), null)), store)
                                .run("orders-load", () -> List.of(split("split-1", "9")), split -> {
                                    materialized.add(split.splitId());
                                    return task(split);
                                }))
                .isInstanceOf(SplitPlanMismatchException.class)
                .hasMessage("split plan changed for job orders-load");
        assertThat(materialized).isEmpty();
    }

    private static BatchWorker worker(List<String> assignments, String failedTaskId) {
        return new BatchWorker() {
            @Override
            public String workerId() {
                return "worker-a";
            }

            @Override
            public WorkerResult execute(BatchTask task) {
                assignments.add(task.taskId());
                if (task.taskId().equals(failedTaskId)) {
                    throw new IllegalStateException("planned task failure: " + task.taskId());
                }
                return new WorkerResult(workerId(), task.taskId(), new SyncResult(1, 1, 1, 1, 7));
            }
        };
    }

    private static BatchTask task(SourceSplit split) {
        return new BatchTask(split, new EmptySource(), new EmptySink(), 2, 1);
    }

    private static SourceSplit split(String splitId, String start) {
        return new SourceSplit(
                splitId, "jdbc:orders", new SplitPosition(Map.of("id", start)), SplitPosition.unbounded());
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
