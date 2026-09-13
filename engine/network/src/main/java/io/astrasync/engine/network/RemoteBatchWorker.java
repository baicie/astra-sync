package io.astrasync.engine.network;

import io.astrasync.engine.observability.DataPlaneLogContext;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchWorker;
import io.astrasync.engine.runtime.CheckpointBatchWorker;
import io.astrasync.engine.runtime.CheckpointExecutionContext;
import io.astrasync.engine.runtime.CheckpointProgressListener;
import io.astrasync.engine.runtime.WorkerResult;
import java.util.Objects;
import java.util.concurrent.Semaphore;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/** BatchWorker adapter with a bounded number of remote tasks in flight. */
public final class RemoteBatchWorker implements BatchWorker, CheckpointBatchWorker {
    private static final Logger LOG = LoggerFactory.getLogger(RemoteBatchWorker.class);

    private final String workerId;
    private final WorkerClient client;
    private final CheckpointWorkerClient checkpointClient;
    private final Semaphore inFlight;

    public RemoteBatchWorker(String workerId, WorkerClient client, int maxInFlightTasks) {
        this.workerId = requireText(workerId, "workerId");
        this.client = Objects.requireNonNull(client, "client must not be null");
        this.checkpointClient = new CheckpointWorkerClient(client.host(), client.port(), client.timeout());
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
        BatchTask checked = Objects.requireNonNull(task, "task must not be null");
        try (DataPlaneLogContext ignored =
                DataPlaneLogContext.open(checked.tenantId(), checked.jobId(), null, workerId, null, null)) {
            LOG.info("remote worker task started");
            try {
                WorkerResult result = executeWithPermit(checked);
                try (DataPlaneLogContext outcome = DataPlaneLogContext.open(
                        checked.tenantId(), checked.jobId(), null, workerId, null, "success")) {
                    LOG.info("remote worker task completed");
                }
                return result;
            } catch (RuntimeException exception) {
                try (DataPlaneLogContext outcome = DataPlaneLogContext.open(
                        checked.tenantId(), checked.jobId(), null, workerId, null, "failure")) {
                    LOG.warn(
                            "remote worker task failed with {}",
                            exception.getClass().getSimpleName());
                }
                throw exception;
            }
        }
    }

    private WorkerResult executeWithPermit(BatchTask task) {
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

    @Override
    public WorkerResult executeCheckpoint(
            CheckpointExecutionContext context, BatchTask task, CheckpointProgressListener progressListener) {
        CheckpointExecutionContext checkedContext = Objects.requireNonNull(context, "context must not be null");
        BatchTask checkedTask = Objects.requireNonNull(task, "task must not be null");
        try (DataPlaneLogContext ignored = DataPlaneLogContext.open(
                checkedTask.tenantId(),
                checkedContext.jobId(),
                checkedContext.executionEpoch(),
                workerId,
                "checkpoint",
                null)) {
            LOG.info("remote worker checkpoint task started");
            try {
                WorkerResult result = checkpointClient.execute(workerId, checkedContext, checkedTask, progressListener);
                try (DataPlaneLogContext outcome = DataPlaneLogContext.open(
                        checkedTask.tenantId(),
                        checkedContext.jobId(),
                        checkedContext.executionEpoch(),
                        workerId,
                        "checkpoint",
                        "success")) {
                    LOG.info("remote worker checkpoint task completed");
                }
                return result;
            } catch (RuntimeException exception) {
                try (DataPlaneLogContext outcome = DataPlaneLogContext.open(
                        checkedTask.tenantId(),
                        checkedContext.jobId(),
                        checkedContext.executionEpoch(),
                        workerId,
                        "checkpoint",
                        "failure")) {
                    LOG.warn(
                            "remote worker checkpoint task failed with {}",
                            exception.getClass().getSimpleName());
                }
                throw exception;
            }
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
