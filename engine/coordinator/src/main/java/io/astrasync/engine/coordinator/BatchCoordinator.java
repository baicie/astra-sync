package io.astrasync.engine.coordinator;

import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.BatchSplitEnumerator;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchWorker;
import io.astrasync.engine.runtime.WorkerResult;
import java.util.ArrayList;
import java.util.HashSet;
import java.util.List;
import java.util.Objects;
import java.util.Set;
import java.util.concurrent.CancellationException;
import java.util.concurrent.ExecutionException;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.Future;
import java.util.concurrent.ThreadFactory;
import java.util.concurrent.atomic.AtomicInteger;

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

    public DistributedRunResult run(BatchSplitEnumerator enumerator) {
        Objects.requireNonNull(enumerator, "enumerator must not be null");
        return run(Objects.requireNonNull(enumerator.enumerate(), "enumerator returned null"));
    }

    public DistributedRunResult run(List<? extends BatchTask> tasks) {
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
        if (copy.isEmpty()) {
            return new DistributedRunResult(List.of(), SyncResult.empty());
        }

        long startedNanos = System.nanoTime();
        List<ExecutorService> queues = new ArrayList<>(workers.size());
        List<Future<WorkerResult>> futures = new ArrayList<>(copy.size());
        try {
            for (int index = 0; index < workers.size(); index++) {
                queues.add(Executors.newSingleThreadExecutor(new CoordinatorThreadFactory(index)));
            }
            for (int index = 0; index < copy.size(); index++) {
                BatchTask task = copy.get(index);
                BatchWorker worker = workers.get(index % workers.size());
                ExecutorService queue = queues.get(index % workers.size());
                futures.add(queue.submit(() -> worker.execute(task)));
            }

            List<WorkerResult> results = new ArrayList<>(copy.size());
            for (Future<WorkerResult> future : futures) {
                try {
                    results.add(future.get());
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

    private static void cancelAll(List<? extends Future<?>> futures) {
        futures.forEach(future -> future.cancel(true));
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
}
