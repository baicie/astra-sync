package io.astrasync.engine.coordinator;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.AdaptiveParallelismPolicy;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskFactory;
import io.astrasync.engine.runtime.BatchWorker;
import io.astrasync.engine.runtime.WorkerResult;
import java.util.List;
import java.util.Map;
import java.util.concurrent.CopyOnWriteArrayList;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;

class BatchCoordinatorTest {
    @Test
    void assignsTasksRoundRobinAndSerializesTasksPerWorker() {
        List<String> assignments = new CopyOnWriteArrayList<>();
        AtomicInteger firstActive = new AtomicInteger();
        AtomicInteger firstMaxActive = new AtomicInteger();
        AtomicInteger secondActive = new AtomicInteger();
        AtomicInteger secondMaxActive = new AtomicInteger();
        BatchWorker first = worker("worker-a", assignments, firstActive, firstMaxActive);
        BatchWorker second = worker("worker-b", assignments, secondActive, secondMaxActive);

        DistributedRunResult result = new BatchCoordinator(List.of(first, second))
                .run(List.of(task("split-1"), task("split-2"), task("split-3")));

        assertThat(assignments).containsExactlyInAnyOrder("worker-a:split-1", "worker-b:split-2", "worker-a:split-3");
        assertThat(result.taskResults())
                .extracting(WorkerResult::taskId)
                .containsExactly("split-1", "split-2", "split-3");
        assertThat(result.metrics().readCount()).isEqualTo(3);
        assertThat(result.metrics().writtenCount()).isEqualTo(3);
        assertThat(firstMaxActive).hasValue(1);
        assertThat(secondMaxActive).hasValue(1);
    }

    @Test
    void rejectsDuplicateTaskIdsBeforeScheduling() {
        List<String> assignments = new CopyOnWriteArrayList<>();
        assertThatThrownBy(() -> new BatchCoordinator(
                                List.of(worker("worker-a", assignments, new AtomicInteger(), new AtomicInteger())))
                        .run(List.of(task("duplicate"), task("duplicate"))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("task id is duplicated: duplicate");
        assertThat(assignments).isEmpty();
    }

    @Test
    void enumeratesSplitsAndMaterializesTasksBeforeScheduling() {
        List<String> materialized = new CopyOnWriteArrayList<>();
        List<String> assignments = new CopyOnWriteArrayList<>();
        SourceSplit first = split("split-1");
        SourceSplit second = split("split-2");
        BatchWorker worker = worker("worker-a", assignments, new AtomicInteger(), new AtomicInteger());
        BatchTaskFactory taskFactory = enumerated -> {
            materialized.add(enumerated.splitId());
            return task(enumerated);
        };

        DistributedRunResult result =
                new BatchCoordinator(List.of(worker)).run(() -> List.of(first, second), taskFactory);

        assertThat(materialized).containsExactly("split-1", "split-2");
        assertThat(assignments).containsExactly("worker-a:split-1", "worker-a:split-2");
        assertThat(result.taskResults()).extracting(WorkerResult::taskId).containsExactly("split-1", "split-2");
    }

    @Test
    void reportsCompletionsAsTheyFinishWhileKeepingResultsInTaskOrder() {
        CountDownLatch slowStarted = new CountDownLatch(1);
        CountDownLatch releaseSlow = new CountDownLatch(1);
        List<String> completions = new CopyOnWriteArrayList<>();
        BatchWorker slow = new BatchWorker() {
            @Override
            public String workerId() {
                return "worker-a";
            }

            @Override
            public WorkerResult execute(BatchTask task) {
                slowStarted.countDown();
                await(releaseSlow);
                return result(workerId(), task);
            }
        };
        BatchWorker fast = new BatchWorker() {
            @Override
            public String workerId() {
                return "worker-b";
            }

            @Override
            public WorkerResult execute(BatchTask task) {
                await(slowStarted);
                return result(workerId(), task);
            }
        };

        DistributedRunResult run = new BatchCoordinator(List.of(slow, fast))
                .run(List.of(task("split-1"), task("split-2")), completed -> {
                    completions.add(completed.taskId());
                    if (completed.taskId().equals("split-2")) {
                        releaseSlow.countDown();
                    }
                });

        assertThat(completions).containsExactly("split-2", "split-1");
        assertThat(run.taskResults()).extracting(WorkerResult::taskId).containsExactly("split-1", "split-2");
    }

    @Test
    void adaptiveSchedulingDoesNotCancelAnInFlightTaskWhenParallelismShrinks() {
        CountDownLatch secondStarted = new CountDownLatch(1);
        CountDownLatch releaseSecond = new CountDownLatch(1);
        AtomicBoolean secondInterrupted = new AtomicBoolean();
        BatchWorker first = new BatchWorker() {
            @Override
            public String workerId() {
                return "worker-a";
            }

            @Override
            public WorkerResult execute(BatchTask task) {
                return new WorkerResult(workerId(), task.taskId(), new SyncResult(1, 1, 1, 1, 200));
            }
        };
        BatchWorker second = new BatchWorker() {
            @Override
            public String workerId() {
                return "worker-b";
            }

            @Override
            public WorkerResult execute(BatchTask task) {
                secondStarted.countDown();
                try {
                    releaseSecond.await(5, TimeUnit.SECONDS);
                } catch (InterruptedException exception) {
                    secondInterrupted.set(true);
                    Thread.currentThread().interrupt();
                    throw new IllegalStateException("in-flight task was cancelled", exception);
                }
                return new WorkerResult(workerId(), task.taskId(), new SyncResult(1, 1, 1, 1, 200));
            }
        };

        DistributedRunResult result = new BatchCoordinator(List.of(first, second))
                .runAdaptive(
                        List.of(task("split-1"), task("split-2"), task("split-3")),
                        AdaptiveParallelismPolicy.adaptive(1, 2, 2, 100, 0),
                        completed -> {
                            if (completed.taskId().equals("split-1")) {
                                await(secondStarted);
                                releaseSecond.countDown();
                            }
                        });

        assertThat(secondInterrupted).isFalse();
        assertThat(result.taskResults())
                .extracting(WorkerResult::taskId)
                .containsExactly("split-1", "split-2", "split-3");
    }

    @Test
    void adaptiveSchedulingPropagatesTheFirstWorkerFailure() {
        BatchWorker failing = new BatchWorker() {
            @Override
            public String workerId() {
                return "worker-a";
            }

            @Override
            public WorkerResult execute(BatchTask task) {
                throw new IllegalStateException("adaptive worker failed");
            }
        };

        assertThatThrownBy(() -> new BatchCoordinator(List.of(failing))
                        .runAdaptive(List.of(task("split-1")), AdaptiveParallelismPolicy.adaptive(1, 1, 1, 100, 0)))
                .isInstanceOf(IllegalStateException.class)
                .hasMessage("adaptive worker failed");
    }

    private static BatchWorker worker(
            String workerId, List<String> assignments, AtomicInteger active, AtomicInteger maxActive) {
        return new BatchWorker() {
            @Override
            public String workerId() {
                return workerId;
            }

            @Override
            public WorkerResult execute(BatchTask task) {
                int current = active.incrementAndGet();
                maxActive.accumulateAndGet(current, Math::max);
                try {
                    assignments.add(workerId + ":" + task.taskId());
                    Thread.yield();
                    return new WorkerResult(workerId, task.taskId(), new SyncResult(1, 1, 1, 1, 0));
                } finally {
                    active.decrementAndGet();
                }
            }
        };
    }

    private static BatchTask task(String taskId) {
        return task(split(taskId));
    }

    private static BatchTask task(SourceSplit split) {
        return new BatchTask(split, new EmptySource(), new EmptySink(), 2, 1);
    }

    private static WorkerResult result(String workerId, BatchTask task) {
        return new WorkerResult(workerId, task.taskId(), new SyncResult(1, 1, 1, 1, 0));
    }

    private static void await(CountDownLatch latch) {
        try {
            if (!latch.await(5, TimeUnit.SECONDS)) {
                throw new IllegalStateException("timed out waiting for coordinated test task");
            }
        } catch (InterruptedException exception) {
            Thread.currentThread().interrupt();
            throw new IllegalStateException("coordinated test task was interrupted", exception);
        }
    }

    private static SourceSplit split(String splitId) {
        return new SourceSplit(splitId, "test-source", new SplitPosition(Map.of("id", "1")), SplitPosition.unbounded());
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
