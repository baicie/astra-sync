package io.astrasync.engine.runtime;

import io.astrasync.connector.api.source.SplitPosition;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.Objects;

/** Worker report for one sink-committed batch awaiting Coordinator durability. */
public record CheckpointProgress(
        String jobId,
        long executionEpoch,
        String taskId,
        long checkpointSequence,
        SplitPosition sourcePosition,
        String sinkCommitToken,
        String batchDigest) {
    public CheckpointProgress {
        jobId = requireText(jobId, "jobId");
        taskId = requireText(taskId, "taskId");
        if (executionEpoch <= 0 || checkpointSequence <= 0) {
            throw new IllegalArgumentException("checkpoint epoch and sequence must be positive");
        }
        sourcePosition = Objects.requireNonNull(sourcePosition, "sourcePosition must not be null");
        sinkCommitToken = requireText(sinkCommitToken, "sinkCommitToken");
        batchDigest = requireText(batchDigest, "batchDigest");
    }

    public String fingerprint() {
        try {
            MessageDigest digest = MessageDigest.getInstance("SHA-256");
            String canonical = jobId + "|" + executionEpoch + "|" + taskId + "|" + checkpointSequence + "|"
                    + sourcePosition.offsets() + "|" + sinkCommitToken + "|" + batchDigest;
            byte[] bytes = digest.digest(canonical.getBytes(StandardCharsets.UTF_8));
            StringBuilder value = new StringBuilder(bytes.length * 2);
            for (byte item : bytes) {
                value.append(Character.forDigit((item >>> 4) & 0xf, 16));
                value.append(Character.forDigit(item & 0xf, 16));
            }
            return value.toString();
        } catch (NoSuchAlgorithmException exception) {
            throw new IllegalStateException("SHA-256 is not available", exception);
        }
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
