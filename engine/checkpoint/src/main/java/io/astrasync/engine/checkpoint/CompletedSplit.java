package io.astrasync.engine.checkpoint;

import io.astrasync.engine.kernel.SyncResult;
import java.util.Objects;

/** First durable successful result for one split. */
public record CompletedSplit(String workerId, String splitFingerprint, SyncResult metrics) {
    public CompletedSplit {
        workerId = requireText(workerId, "workerId");
        splitFingerprint = requireText(splitFingerprint, "splitFingerprint");
        metrics = Objects.requireNonNull(metrics, "metrics must not be null");
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
