package io.astrasync.engine.runtime;

/** Configuration for adapting the source batch size. */
public record AdaptiveBatchPolicy(
        int minBatchRecords, int initialBatchRecords, long targetBatchNanos, int adjustmentCooldownSamples) {
    public AdaptiveBatchPolicy {
        if (minBatchRecords <= 0) {
            throw new IllegalArgumentException("minBatchRecords must be positive");
        }
        if (initialBatchRecords < minBatchRecords) {
            throw new IllegalArgumentException("initialBatchRecords must not be below minBatchRecords");
        }
        if (targetBatchNanos < 0) {
            throw new IllegalArgumentException("targetBatchNanos must not be negative");
        }
        if (adjustmentCooldownSamples < 0) {
            throw new IllegalArgumentException("adjustmentCooldownSamples must not be negative");
        }
    }

    public static AdaptiveBatchPolicy fixed(int maxBatchRecords) {
        if (maxBatchRecords <= 0) {
            throw new IllegalArgumentException("maxBatchRecords must be positive");
        }
        return new AdaptiveBatchPolicy(maxBatchRecords, maxBatchRecords, 0, 0);
    }

    public static AdaptiveBatchPolicy adaptive(
            int minBatchRecords, int initialBatchRecords, long targetBatchNanos, int adjustmentCooldownSamples) {
        return new AdaptiveBatchPolicy(
                minBatchRecords, initialBatchRecords, targetBatchNanos, adjustmentCooldownSamples);
    }

    public boolean enabled() {
        return targetBatchNanos > 0;
    }

    public AdaptiveBatchPolicy limitedTo(int maxBatchRecords) {
        if (maxBatchRecords <= 0) {
            throw new IllegalArgumentException("maxBatchRecords must be positive");
        }
        return new AdaptiveBatchPolicy(
                Math.min(minBatchRecords, maxBatchRecords),
                Math.min(initialBatchRecords, maxBatchRecords),
                targetBatchNanos,
                adjustmentCooldownSamples);
    }
}
