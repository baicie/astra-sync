package io.astrasync.engine.runtime;

import java.util.Objects;

/** Thread-safe EWMA controller for the number of Workers receiving new tasks. */
public final class AdaptiveParallelismController {
    private static final double OLD_WEIGHT = 0.75;
    private static final double NEW_WEIGHT = 0.25;
    private static final double SLOW_MULTIPLIER = 1.25;
    private static final double FAST_MULTIPLIER = 0.75;

    private final AdaptiveParallelismPolicy policy;
    private int currentParallelism;
    private int cooldownRemaining;
    private double ewmaNanos = -1;

    public AdaptiveParallelismController(AdaptiveParallelismPolicy policy, int availableWorkers) {
        this.policy = Objects.requireNonNull(policy, "policy must not be null").limitedTo(availableWorkers);
        this.currentParallelism = this.policy.initialParallelism();
    }

    public synchronized int currentParallelism() {
        return currentParallelism;
    }

    public synchronized double ewmaNanos() {
        return ewmaNanos;
    }

    public synchronized int cooldownRemaining() {
        return cooldownRemaining;
    }

    public synchronized void observe(AdaptiveParallelismSample sample) {
        Objects.requireNonNull(sample, "sample must not be null");
        if (!policy.enabled()) {
            return;
        }
        double observedNanos = sample.taskElapsedNanos();
        ewmaNanos = ewmaNanos < 0 ? observedNanos : OLD_WEIGHT * ewmaNanos + NEW_WEIGHT * observedNanos;
        if (cooldownRemaining > 0) {
            cooldownRemaining--;
            return;
        }

        int next = currentParallelism;
        if (ewmaNanos >= policy.targetTaskNanos() * SLOW_MULTIPLIER) {
            next = Math.max(policy.minParallelism(), currentParallelism - 1);
        } else if (sample.queuedTasks() > 0 && ewmaNanos <= policy.targetTaskNanos() * FAST_MULTIPLIER) {
            next = Math.min(policy.maxParallelism(), currentParallelism + 1);
        }
        if (next != currentParallelism) {
            currentParallelism = next;
            cooldownRemaining = policy.adjustmentCooldownSamples();
        }
    }
}
