package io.astrasync.engine.jobspec;

/** Strict JobSpec representation of an optional adaptive parallelism policy. */
public record AdaptiveParallelismSpec(
        int minParallelism,
        int initialParallelism,
        int maxParallelism,
        long targetTaskNanos,
        int adjustmentCooldownSamples) {
    public AdaptiveParallelismSpec {
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
                && (minParallelism != 1
                        || initialParallelism != 1
                        || maxParallelism != 1
                        || adjustmentCooldownSamples != 0)) {
            throw new IllegalArgumentException(
                    "disabled adaptive parallelism spec must use one worker and zero cooldown");
        }
    }

    public static AdaptiveParallelismSpec disabled() {
        return new AdaptiveParallelismSpec(1, 1, 1, 0, 0);
    }

    public boolean enabled() {
        return targetTaskNanos > 0;
    }
}
