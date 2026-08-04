package io.astrasync.engine.worker;

import io.astrasync.engine.network.WorkerServer;
import io.astrasync.engine.runtime.BatchTaskFactory;
import java.util.Objects;
import java.util.concurrent.CountDownLatch;

/** Lifecycle wrapper for an operational Worker protocol server. */
public final class WorkerService implements AutoCloseable {
    private final WorkerServer server;
    private final CountDownLatch stopped = new CountDownLatch(1);
    private volatile boolean started;

    public WorkerService(WorkerConfiguration configuration, BatchTaskFactory taskFactory) {
        WorkerConfiguration checked = Objects.requireNonNull(configuration, "configuration must not be null");
        this.server = new WorkerServer(
                checked.workerId(),
                checked.port(),
                Objects.requireNonNull(taskFactory, "taskFactory must not be null"),
                new InProcessBatchWorker(checked.workerId()),
                checked.maxConcurrentTasks(),
                checked.taskQueueCapacity(),
                checked.maxConnections());
    }

    public synchronized void start() {
        if (started) {
            throw new IllegalStateException("Worker service is already started");
        }
        server.start();
        started = true;
    }

    public int port() {
        if (!started) {
            throw new IllegalStateException("Worker service is not started");
        }
        return server.port();
    }

    public void await() throws InterruptedException {
        stopped.await();
    }

    @Override
    public synchronized void close() {
        if (started) {
            started = false;
            server.close();
        }
        stopped.countDown();
    }
}
