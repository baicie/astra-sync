package io.astrasync.engine.runtime;

/** Immutable bounds and target used by adaptive split scheduling. */
public record AdaptiveParallelismPolicy(
        int minParallelism,
        int initialParallelism,
        int maxParallelism,
        long targetTaskNanos,
        int adjustmentCooldownSamples) {
    public AdaptiveParallelismPolicy {
        if (minParallelism <= 0) {
            throw new IllegalArgumentException("minParallelism must be positive");
        }
        if (initialParallelism < minParallelism) {
            throw new IllegalArgumentException("initialParallelism must not be below minParallelism");
        }
        if (maxParallelism < initialParallelism) {
            throw new IllegalArgumentException("maxParallelism must not be below initialParallelism");
        }
        if (targetTaskNanos < 0) {
            throw new IllegalArgumentException("targetTaskNanos must not be negative");
        }
        if (adjustmentCooldownSamples < 0) {
            throw new IllegalArgumentException("adjustmentCooldownSamples must not be negative");
        }
        if (targetTaskNanos == 0
                && (minParallelism != initialParallelism
                        || initialParallelism != maxParallelism
                        || adjustmentCooldownSamples != 0)) {
            throw new IllegalArgumentException("disabled parallelism policy must use fixed bounds and zero cooldown");
        }
    }

    public static AdaptiveParallelismPolicy fixed(int parallelism) {
        if (parallelism <= 0) {
            throw new IllegalArgumentException("parallelism must be positive");
        }
        return new AdaptiveParallelismPolicy(parallelism, parallelism, parallelism, 0, 0);
    }

    public static AdaptiveParallelismPolicy adaptive(
            int minParallelism,
            int initialParallelism,
            int maxParallelism,
            long targetTaskNanos,
            int adjustmentCooldownSamples) {
        return new AdaptiveParallelismPolicy(
                minParallelism, initialParallelism, maxParallelism, targetTaskNanos, adjustmentCooldownSamples);
    }

    public boolean enabled() {
        return targetTaskNanos > 0;
    }

    public AdaptiveParallelismPolicy limitedTo(int availableWorkers) {
        if (availableWorkers <= 0) {
            throw new IllegalArgumentException("availableWorkers must be positive");
        }
        int limitedMax = Math.min(maxParallelism, availableWorkers);
        if (limitedMax < minParallelism) {
            throw new IllegalArgumentException("availableWorkers is below minParallelism");
        }
        return new AdaptiveParallelismPolicy(
                minParallelism,
                Math.min(initialParallelism, limitedMax),
                limitedMax,
                targetTaskNanos,
                adjustmentCooldownSamples);
    }
}
