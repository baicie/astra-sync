package io.astrasync.engine.checkpoint;

import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.WorkerResult;
import java.util.Objects;

/** Durable terminal state for one checkpoint-aware split. */
public record CheckpointCompletion(
        int formatVersion,
        String jobId,
        long executionEpoch,
        String splitId,
        String splitFingerprint,
        long checkpointSequence,
        String workerId,
        SyncResult metrics) {
    public static final int CURRENT_FORMAT_VERSION = 1;

    public CheckpointCompletion {
        if (formatVersion != CURRENT_FORMAT_VERSION) {
            throw new IllegalArgumentException("unsupported checkpoint completion format: " + formatVersion);
        }
        jobId = requireText(jobId, "jobId");
        splitId = requireText(splitId, "splitId");
        splitFingerprint = requireText(splitFingerprint, "splitFingerprint");
        workerId = requireText(workerId, "workerId");
        if (executionEpoch <= 0) {
            throw new IllegalArgumentException("executionEpoch must be positive");
        }
        if (checkpointSequence < 0) {
            throw new IllegalArgumentException("checkpointSequence must not be negative");
        }
        metrics = Objects.requireNonNull(metrics, "metrics must not be null");
    }

    public CheckpointCompletion(
            String jobId,
            long executionEpoch,
            String splitId,
            String splitFingerprint,
            long checkpointSequence,
            WorkerResult result) {
        this(
                CURRENT_FORMAT_VERSION,
                jobId,
                executionEpoch,
                splitId,
                splitFingerprint,
                checkpointSequence,
                Objects.requireNonNull(result, "result must not be null").workerId(),
                result.metrics());
        if (!splitId.equals(result.taskId())) {
            throw new IllegalArgumentException("Worker result changed split identity: " + result.taskId());
        }
    }

    public WorkerResult workerResult() {
        return new WorkerResult(workerId, splitId, metrics);
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
