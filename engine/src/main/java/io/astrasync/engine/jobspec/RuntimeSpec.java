package io.astrasync.engine.jobspec;

public record RuntimeSpec(int maxBatchRecords) {
    public static final int DEFAULT_MAX_BATCH_RECORDS = 1_024;

    public RuntimeSpec {
        if (maxBatchRecords <= 0) {
            throw new IllegalArgumentException("maxBatchRecords must be positive");
        }
    }

    public static RuntimeSpec defaults() {
        return new RuntimeSpec(DEFAULT_MAX_BATCH_RECORDS);
    }
}
