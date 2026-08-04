package io.astrasync.engine.checkpoint;

import io.astrasync.connector.api.source.SplitPosition;
import java.util.Objects;

/** Durable, ordered progress for one committed source batch. */
public record CheckpointRecord(
        int formatVersion,
        String jobId,
        long executionEpoch,
        String splitId,
        String splitFingerprint,
        long checkpointSequence,
        SplitPosition sourcePosition,
        String sinkCommitToken,
        String batchDigest) {
    public static final int CURRENT_FORMAT_VERSION = 1;

    public CheckpointRecord {
        if (formatVersion != CURRENT_FORMAT_VERSION) {
            throw new IllegalArgumentException("unsupported checkpoint format: " + formatVersion);
        }
        jobId = requireText(jobId, "jobId");
        splitId = requireText(splitId, "splitId");
        splitFingerprint = requireText(splitFingerprint, "splitFingerprint");
        if (executionEpoch <= 0 || checkpointSequence <= 0) {
            throw new IllegalArgumentException("checkpoint epoch and sequence must be positive");
        }
        sourcePosition = Objects.requireNonNull(sourcePosition, "sourcePosition must not be null");
        sinkCommitToken = requireText(sinkCommitToken, "sinkCommitToken");
        batchDigest = requireText(batchDigest, "batchDigest");
    }

    public CheckpointRecord(
            String jobId,
            long executionEpoch,
            String splitId,
            String splitFingerprint,
            long checkpointSequence,
            SplitPosition sourcePosition,
            String sinkCommitToken,
            String batchDigest) {
        this(
                CURRENT_FORMAT_VERSION,
                jobId,
                executionEpoch,
                splitId,
                splitFingerprint,
                checkpointSequence,
                sourcePosition,
                sinkCommitToken,
                batchDigest);
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
