package io.astrasync.engine.jobspec;

public record RuntimeSpec(
        int maxBatchRecords, AdaptiveBatchSpec adaptiveBatch, AdaptiveParallelismSpec adaptiveParallelism) {
    public static final int DEFAULT_MAX_BATCH_RECORDS = 1_024;

    public RuntimeSpec(int maxBatchRecords) {
        this(maxBatchRecords, AdaptiveBatchSpec.disabled(maxBatchRecords), AdaptiveParallelismSpec.disabled());
    }

    public RuntimeSpec {
        if (maxBatchRecords <= 0) {
            throw new IllegalArgumentException("maxBatchRecords must be positive");
        }
        if (adaptiveBatch == null) {
            throw new IllegalArgumentException("adaptiveBatch must not be null");
        }
        if (adaptiveBatch.minBatchRecords() > maxBatchRecords
                || adaptiveBatch.initialBatchRecords() > maxBatchRecords) {
            throw new IllegalArgumentException("adaptive batch bounds must not exceed maxBatchRecords");
        }
        if (!adaptiveBatch.enabled() && adaptiveBatch.minBatchRecords() != maxBatchRecords) {
            throw new IllegalArgumentException("disabled adaptive batch must use maxBatchRecords as its bound");
        }
        if (adaptiveParallelism == null) {
            throw new IllegalArgumentException("adaptiveParallelism must not be null");
        }
    }

    public static RuntimeSpec defaults() {
        return new RuntimeSpec(DEFAULT_MAX_BATCH_RECORDS);
    }
}
