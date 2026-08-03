package io.astrasync.engine.network;

import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchWorker;
import io.astrasync.engine.runtime.WorkerResult;
import java.util.Objects;
import java.util.concurrent.Semaphore;

/** BatchWorker adapter with a bounded number of remote tasks in flight. */
public final class RemoteBatchWorker implements BatchWorker {
    private final String workerId;
    private final WorkerClient client;
    private final Semaphore inFlight;

    public RemoteBatchWorker(String workerId, WorkerClient client, int maxInFlightTasks) {
        this.workerId = requireText(workerId, "workerId");
        this.client = Objects.requireNonNull(client, "client must not be null");
        if (maxInFlightTasks <= 0) {
            throw new IllegalArgumentException("maxInFlightTasks must be positive");
        }
        this.inFlight = new Semaphore(maxInFlightTasks);
    }

    @Override
    public String workerId() {
        return workerId;
    }

    @Override
    public WorkerResult execute(BatchTask task) {
        Objects.requireNonNull(task, "task must not be null");
        try {
            inFlight.acquire();
        } catch (InterruptedException exception) {
            Thread.currentThread().interrupt();
            throw new NetworkWorkerException("remote Worker permit acquisition interrupted", exception);
        }
        try {
            return client.execute(workerId, task);
        } finally {
            inFlight.release();
        }
    }

    public boolean cancel(String taskId, String reason) {
        return client.cancel(workerId, taskId, reason);
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
