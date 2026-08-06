package io.astrasync.engine.jobspec;

/** Strict JobSpec representation of an optional adaptive batch policy. */
public record AdaptiveBatchSpec(
        int minBatchRecords, int initialBatchRecords, long targetBatchNanos, int adjustmentCooldownSamples) {
    public AdaptiveBatchSpec {
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
        if (targetBatchNanos == 0 && (initialBatchRecords != minBatchRecords || adjustmentCooldownSamples != 0)) {
            throw new IllegalArgumentException("disabled adaptive batch spec must use fixed bounds and zero cooldown");
        }
    }

    public static AdaptiveBatchSpec disabled(int maxBatchRecords) {
        if (maxBatchRecords <= 0) {
            throw new IllegalArgumentException("maxBatchRecords must be positive");
        }
        return new AdaptiveBatchSpec(maxBatchRecords, maxBatchRecords, 0, 0);
    }

    public boolean enabled() {
        return targetBatchNanos > 0;
    }
}
