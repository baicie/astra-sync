package io.astrasync.engine.kernel;

public record SyncResult(
        long readCount, long writtenCount, long batchCount, int maxObservedBatchSize, long elapsedNanos) {
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
}
