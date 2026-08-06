package io.astrasync.engine.coordinator;

import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitEnumerator;
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.AdaptiveParallelismController;
import io.astrasync.engine.runtime.AdaptiveParallelismPolicy;
import io.astrasync.engine.runtime.AdaptiveParallelismSample;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskFactory;
import io.astrasync.engine.runtime.BatchWorker;
import io.astrasync.engine.runtime.WorkerResult;
import java.util.ArrayList;
import java.util.Collections;
import java.util.HashSet;
import java.util.List;
import java.util.Objects;
import java.util.Set;
import java.util.concurrent.BlockingQueue;
import java.util.concurrent.CancellationException;
import java.util.concurrent.CompletionService;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorCompletionService;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.LinkedBlockingQueue;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.atomic.AtomicBoolean;
import java.util.concurrent.atomic.AtomicInteger;
import java.util.function.Consumer;

/** Assigns independent batch tasks to Workers with one serialized queue per Worker. */
public final class BatchCoordinator {
    private final List<BatchWorker> workers;

    public BatchCoordinator(List<? extends BatchWorker> workers) {
        Objects.requireNonNull(workers, "workers must not be null");
        if (workers.isEmpty()) {
            throw new IllegalArgumentException("at least one worker is required");
        }
        List<BatchWorker> copy = new ArrayList<>(workers.size());
        Set<String> workerIds = new HashSet<>();
        for (BatchWorker worker : workers) {
            Objects.requireNonNull(worker, "workers must not contain null");
            if (!workerIds.add(worker.workerId())) {
                throw new IllegalArgumentException("worker id is duplicated: " + worker.workerId());
            }
            copy.add(worker);
        }
        this.workers = List.copyOf(copy);
    }

    public DistributedRunResult run(SplitEnumerator enumerator, BatchTaskFactory taskFactory) {
        Objects.requireNonNull(enumerator, "enumerator must not be null");
        Objects.requireNonNull(taskFactory, "taskFactory must not be null");
        List<SourceSplit> splits = Objects.requireNonNull(enumerator.enumerate(), "enumerator returned null");
        Set<String> splitIds = new HashSet<>();
        List<BatchTask> tasks = new ArrayList<>(splits.size());
        for (SourceSplit split : splits) {
            Objects.requireNonNull(split, "enumerator returned a null split");
            if (!splitIds.add(split.splitId())) {
                throw new IllegalArgumentException("split id is duplicated: " + split.splitId());
            }
            BatchTask task = Objects.requireNonNull(taskFactory.create(split), "task factory returned null");
            if (!split.equals(task.split())) {
                throw new IllegalArgumentException("task factory changed split " + split.splitId());
            }
            tasks.add(task);
        }
        return run(tasks);
    }

    public DistributedRunResult run(List<? extends BatchTask> tasks) {
        return run(tasks, ignored -> {});
    }

    public DistributedRunResult run(
            List<? extends BatchTask> tasks, Consumer<? super WorkerResult> completionListener) {
        Objects.requireNonNull(completionListener, "completionListener must not be null");
        List<BatchTask> copy = validateTasks(tasks);
        if (copy.isEmpty()) {
            return new DistributedRunResult(List.of(), SyncResult.empty());
        }

        long startedNanos = System.nanoTime();
        List<ExecutorService> queues = new ArrayList<>(workers.size());
        List<CompletionService<IndexedWorkerResult>> completionServices = new ArrayList<>(workers.size());
        BlockingQueue<Future<IndexedWorkerResult>> completions = new LinkedBlockingQueue<>();
        List<Future<IndexedWorkerResult>> futures = new ArrayList<>(copy.size());
        AtomicBoolean taskFailed = new AtomicBoolean();
        try {
            for (int index = 0; index < workers.size(); index++) {
                ExecutorService queue = Executors.newSingleThreadExecutor(new CoordinatorThreadFactory(index));
                queues.add(queue);
                completionServices.add(new ExecutorCompletionService<>(queue, completions));
            }
            for (int index = 0; index < copy.size(); index++) {
                BatchTask task = copy.get(index);
                BatchWorker worker = workers.get(index % workers.size());
                int taskIndex = index;
                futures.add(completionServices
                        .get(index % workers.size())
                        .submit(() -> execute(taskIndex, worker, task, taskFailed)));
            }

            List<WorkerResult> results = new ArrayList<>(Collections.nCopies(copy.size(), null));
            for (int completed = 0; completed < copy.size(); completed++) {
                try {
                    IndexedWorkerResult indexed = completions.take().get();
                    if (indexed.skipped()) {
                        continue;
                    }
                    if (indexed.failure() != null) {
                        cancelAll(futures);
                        throw indexed.failure();
                    }
                    try {
                        completionListener.accept(indexed.result());
                    } catch (RuntimeException exception) {
                        taskFailed.set(true);
                        cancelAll(futures);
                        throw new BatchCoordinatorException(
                                "task completion listener failed for "
                                        + indexed.result().taskId(),
                                exception);
                    }
                    results.set(indexed.index(), indexed.result());
                } catch (InterruptedException exception) {
                    cancelAll(futures);
                    Thread.currentThread().interrupt();
                    throw new BatchCoordinatorException("coordinator interrupted", exception);
                } catch (CancellationException exception) {
                    cancelAll(futures);
                    throw new BatchCoordinatorException("coordinator cancelled", exception);
                } catch (ExecutionException exception) {
                    cancelAll(futures);
                    Throwable cause = exception.getCause();
                    if (cause instanceof RuntimeException runtimeException) {
                        throw runtimeException;
                    }
                    throw new BatchCoordinatorException("worker execution failed", cause);
                }
            }
            return new DistributedRunResult(results, aggregate(results, startedNanos));
        } finally {
            cancelAll(futures);
            queues.forEach(ExecutorService::shutdownNow);
        }
    }

    public DistributedRunResult runAdaptive(List<? extends BatchTask> tasks, AdaptiveParallelismPolicy policy) {
        return runAdaptive(tasks, policy, ignored -> {});
    }

    public DistributedRunResult runAdaptive(
            List<? extends BatchTask> tasks,
            AdaptiveParallelismPolicy policy,
            Consumer<? super WorkerResult> completionListener) {
        List<BatchTask> copy = validateTasks(tasks);
        Objects.requireNonNull(policy, "policy must not be null");
        Objects.requireNonNull(completionListener, "completionListener must not be null");
        if (copy.isEmpty()) {
            return new DistributedRunResult(List.of(), SyncResult.empty());
        }

        AdaptiveParallelismController controller = new AdaptiveParallelismController(policy, workers.size());
        long startedNanos = System.nanoTime();
        List<ExecutorService> queues = new ArrayList<>(workers.size());
        List<CompletionService<AdaptiveIndexedWorkerResult>> completionServices = new ArrayList<>(workers.size());
        BlockingQueue<Future<AdaptiveIndexedWorkerResult>> completions = new LinkedBlockingQueue<>();
        List<Future<AdaptiveIndexedWorkerResult>> futures = new ArrayList<>(copy.size());
        boolean[] busyWorkers = new boolean[workers.size()];
        List<WorkerResult> results = new ArrayList<>(Collections.nCopies(copy.size(), null));
        int nextTaskIndex = 0;
        int activeTasks = 0;
        int completedTasks = 0;
        int nextWorkerIndex = 0;
        try {
            for (int index = 0; index < workers.size(); index++) {
                ExecutorService queue = Executors.newSingleThreadExecutor(new CoordinatorThreadFactory(index));
                queues.add(queue);
                completionServices.add(new ExecutorCompletionService<>(queue, completions));
            }
            while (completedTasks < copy.size()) {
                while (nextTaskIndex < copy.size() && activeTasks < controller.currentParallelism()) {
                    int workerIndex = nextIdleWorker(busyWorkers, nextWorkerIndex);
                    if (workerIndex < 0) {
                        break;
                    }
                    BatchTask task = copy.get(nextTaskIndex);
                    int taskIndex = nextTaskIndex++;
                    busyWorkers[workerIndex] = true;
                    activeTasks++;
                    nextWorkerIndex = (workerIndex + 1) % workers.size();
                    futures.add(completionServices
                            .get(workerIndex)
                            .submit(() -> executeAdaptive(taskIndex, workerIndex, workers.get(workerIndex), task)));
                }

                AdaptiveIndexedWorkerResult indexed = completions.take().get();
                busyWorkers[indexed.workerIndex()] = false;
                activeTasks--;
                completedTasks++;
                if (indexed.failure() != null) {
                    cancelAll(futures);
                    throw indexed.failure();
                }
                try {
                    completionListener.accept(indexed.result());
                } catch (RuntimeException exception) {
                    cancelAll(futures);
                    throw new BatchCoordinatorException(
                            "task completion listener failed for "
                                    + indexed.result().taskId(),
                            exception);
                }
                results.set(indexed.index(), indexed.result());
                controller.observe(new AdaptiveParallelismSample(
                        indexed.result().metrics().elapsedNanos(),
                        copy.size() - nextTaskIndex,
                        Math.max(1, activeTasks)));
            }
            return new DistributedRunResult(results, aggregate(results, startedNanos));
        } catch (InterruptedException exception) {
            cancelAll(futures);
            Thread.currentThread().interrupt();
            throw new BatchCoordinatorException("coordinator interrupted", exception);
        } catch (CancellationException exception) {
            cancelAll(futures);
            throw new BatchCoordinatorException("coordinator cancelled", exception);
        } catch (ExecutionException exception) {
            cancelAll(futures);
            Throwable cause = exception.getCause();
            if (cause instanceof RuntimeException runtimeException) {
                throw runtimeException;
            }
            throw new BatchCoordinatorException("worker execution failed", cause);
        } finally {
            cancelAll(futures);
            queues.forEach(ExecutorService::shutdownNow);
        }
    }

    private List<BatchTask> validateTasks(List<? extends BatchTask> tasks) {
        Objects.requireNonNull(tasks, "tasks must not be null");
        List<BatchTask> copy = new ArrayList<>(tasks.size());
        Set<String> taskIds = new HashSet<>();
        for (BatchTask task : tasks) {
            Objects.requireNonNull(task, "tasks must not contain null");
            if (!taskIds.add(task.taskId())) {
                throw new IllegalArgumentException("task id is duplicated: " + task.taskId());
            }
            copy.add(task);
        }
        return copy;
    }

    private static int nextIdleWorker(boolean[] busyWorkers, int startingIndex) {
        for (int offset = 0; offset < busyWorkers.length; offset++) {
            int index = (startingIndex + offset) % busyWorkers.length;
            if (!busyWorkers[index]) {
                return index;
            }
        }
        return -1;
    }

    private static AdaptiveIndexedWorkerResult executeAdaptive(
            int index, int workerIndex, BatchWorker worker, BatchTask task) {
        try {
            WorkerResult result = Objects.requireNonNull(worker.execute(task), "Worker returned null result");
            return new AdaptiveIndexedWorkerResult(index, workerIndex, result, null);
        } catch (RuntimeException exception) {
            return new AdaptiveIndexedWorkerResult(index, workerIndex, null, exception);
        }
    }

    private static void cancelAll(List<? extends Future<?>> futures) {
        futures.forEach(future -> future.cancel(true));
    }

    private static IndexedWorkerResult execute(
            int index, BatchWorker worker, BatchTask task, AtomicBoolean taskFailed) {
        if (taskFailed.get()) {
            return new IndexedWorkerResult(index, null, null, true);
        }
        try {
            WorkerResult result = Objects.requireNonNull(worker.execute(task), "Worker returned null result");
            return new IndexedWorkerResult(index, result, null, false);
        } catch (RuntimeException exception) {
            taskFailed.set(true);
            return new IndexedWorkerResult(index, null, exception, false);
        }
    }

    private static SyncResult aggregate(List<WorkerResult> results, long startedNanos) {
        long readCount = 0;
        long writtenCount = 0;
        long batchCount = 0;
        int maxObservedBatchSize = 0;
        for (WorkerResult result : results) {
            SyncResult metrics = result.metrics();
            readCount += metrics.readCount();
            writtenCount += metrics.writtenCount();
            batchCount += metrics.batchCount();
            maxObservedBatchSize = Math.max(maxObservedBatchSize, metrics.maxObservedBatchSize());
        }
        return new SyncResult(
                readCount,
                writtenCount,
                batchCount,
                maxObservedBatchSize,
                Math.max(0, System.nanoTime() - startedNanos));
    }

    private static final class CoordinatorThreadFactory implements ThreadFactory {
        private final int workerQueue;
        private final AtomicInteger sequence = new AtomicInteger();

        private CoordinatorThreadFactory(int workerQueue) {
            this.workerQueue = workerQueue;
        }

        @Override
        public Thread newThread(Runnable runnable) {
            Thread thread = new Thread(
                    runnable, "astrasync-coordinator-worker-" + workerQueue + "-" + sequence.incrementAndGet());
            thread.setDaemon(true);
            return thread;
        }
    }

    private record IndexedWorkerResult(int index, WorkerResult result, RuntimeException failure, boolean skipped) {}

    private record AdaptiveIndexedWorkerResult(
            int index, int workerIndex, WorkerResult result, RuntimeException failure) {}
}
