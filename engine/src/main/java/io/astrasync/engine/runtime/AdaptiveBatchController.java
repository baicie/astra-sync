package io.astrasync.engine.runtime;

import java.util.Objects;

/** Thread-safe EWMA controller for the next bounded source batch limit. */
public final class AdaptiveBatchController {
    private static final double OLD_WEIGHT = 0.75;
    private static final double NEW_WEIGHT = 0.25;
    private static final double SLOW_MULTIPLIER = 1.25;
    private static final double FAST_MULTIPLIER = 0.75;

    private final AdaptiveBatchPolicy policy;
    private final int maxBatchRecords;
    private int currentBatchRecords;
    private int cooldownRemaining;
    private double ewmaNanos = -1;

    public AdaptiveBatchController(AdaptiveBatchPolicy policy, int maxBatchRecords) {
        this.policy = Objects.requireNonNull(policy, "policy must not be null");
        if (maxBatchRecords <= 0) {
            throw new IllegalArgumentException("maxBatchRecords must be positive");
        }
        if (policy.initialBatchRecords() > maxBatchRecords) {
            throw new IllegalArgumentException("initialBatchRecords must not exceed maxBatchRecords");
        }
        if (policy.minBatchRecords() > maxBatchRecords) {
            throw new IllegalArgumentException("minBatchRecords must not exceed maxBatchRecords");
        }
        this.maxBatchRecords = maxBatchRecords;
        this.currentBatchRecords = policy.initialBatchRecords();
    }

    public synchronized int currentBatchRecords() {
        return currentBatchRecords;
    }

    public synchronized double ewmaNanos() {
        return ewmaNanos;
    }

    public synchronized int cooldownRemaining() {
        return cooldownRemaining;
    }

    public synchronized void observe(AdaptiveBatchSample sample) {
        Objects.requireNonNull(sample, "sample must not be null");
        if (!policy.enabled() || sample.records() == 0) {
            return;
        }
        double observedNanos = (double) sample.processingNanos() + sample.queueWaitNanos();
        ewmaNanos = ewmaNanos < 0 ? observedNanos : OLD_WEIGHT * ewmaNanos + NEW_WEIGHT * observedNanos;
        if (cooldownRemaining > 0) {
            cooldownRemaining--;
            return;
        }

        boolean exchangeFull = sample.queueDepth() == sample.queueCapacity();
        boolean slow = ewmaNanos >= policy.targetBatchNanos() * SLOW_MULTIPLIER;
        boolean fast = ewmaNanos <= policy.targetBatchNanos() * FAST_MULTIPLIER;
        int next = currentBatchRecords;
        if (exchangeFull || slow) {
            next = Math.max(policy.minBatchRecords(), currentBatchRecords / 2);
        } else if (sample.queueDepth() == 0 && fast) {
            next = Math.min(maxBatchRecords, doubleWithinLimit(currentBatchRecords));
        }
        if (next != currentBatchRecords) {
            currentBatchRecords = next;
            cooldownRemaining = policy.adjustmentCooldownSamples();
        }
    }

    private int doubleWithinLimit(int value) {
        return value > maxBatchRecords / 2 ? maxBatchRecords : value * 2;
    }
}
