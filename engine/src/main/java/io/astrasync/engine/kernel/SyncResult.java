package io.astrasync.engine.kernel;

import java.io.Serial;
import java.io.Serializable;

public record SyncResult(
        long readCount, long writtenCount, long batchCount, int maxObservedBatchSize, long elapsedNanos)
        implements Serializable {
    @Serial
    private static final long serialVersionUID = 1L;

    public SyncResult {
        if (readCount < 0
                || writtenCount < 0
                || writtenCount > readCount
                || batchCount < 0
                || maxObservedBatchSize < 0
                || elapsedNanos < 0) {
            throw new IllegalArgumentException("invalid sync metrics");
        }
    }

    public static SyncResult empty() {
        return new SyncResult(0, 0, 0, 0, 0);
    }
}
