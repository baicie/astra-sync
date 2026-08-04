package io.astrasync.engine.runtime;

import io.astrasync.connector.api.source.SplitPosition;
import java.util.Objects;

/** Immutable task identity and resume cursor for one checkpoint-aware execution. */
public record CheckpointExecutionContext(
        String jobId,
        long executionEpoch,
        String splitId,
        String splitFingerprint,
        long checkpointSequence,
        SplitPosition sourcePosition,
        EpochFence epochFence) {
    public CheckpointExecutionContext {
        jobId = requireText(jobId, "jobId");
        splitId = requireText(splitId, "splitId");
        splitFingerprint = requireText(splitFingerprint, "splitFingerprint");
        if (executionEpoch <= 0) {
            throw new IllegalArgumentException("executionEpoch must be positive");
        }
        if (checkpointSequence < 0) {
            throw new IllegalArgumentException("checkpointSequence must not be negative");
        }
        sourcePosition = Objects.requireNonNull(sourcePosition, "sourcePosition must not be null");
        epochFence = Objects.requireNonNull(epochFence, "epochFence must not be null");
    }

    public CheckpointExecutionContext(
            String jobId,
            long executionEpoch,
            String splitId,
            long checkpointSequence,
            SplitPosition sourcePosition,
            EpochFence epochFence) {
        this(jobId, executionEpoch, splitId, "unknown", checkpointSequence, sourcePosition, epochFence);
    }

    public void assertCurrent() {
        epochFence.assertCurrent(jobId, executionEpoch);
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
