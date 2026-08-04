package io.astrasync.connector.api;

import java.util.Objects;

/** Immutable identity supplied to checkpoint-aware connector resources. */
public record CheckpointContext(String jobId, long executionEpoch, String splitId, Runnable epochAssertion) {
    public CheckpointContext(String jobId, long executionEpoch, String splitId) {
        this(jobId, executionEpoch, splitId, () -> {});
    }

    public CheckpointContext {
        jobId = requireText(jobId, "jobId");
        splitId = requireText(splitId, "splitId");
        if (executionEpoch <= 0) {
            throw new IllegalArgumentException("executionEpoch must be positive");
        }
        epochAssertion = Objects.requireNonNull(epochAssertion, "epochAssertion must not be null");
    }

    public void assertCurrent() {
        epochAssertion.run();
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
