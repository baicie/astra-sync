package io.astrasync.connector.api;

import java.util.Objects;

/** Stable logical identity supplied to an exactly-once sink for one source batch. */
public record SinkCommitContext(
        String jobId, String splitId, long checkpointSequence, String batchDigest, String commitToken) {
    public SinkCommitContext {
        jobId = requireText(jobId, "jobId");
        splitId = requireText(splitId, "splitId");
        if (checkpointSequence <= 0) {
            throw new IllegalArgumentException("checkpointSequence must be positive");
        }
        batchDigest = requireText(batchDigest, "batchDigest");
        commitToken = requireText(commitToken, "commitToken");
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
