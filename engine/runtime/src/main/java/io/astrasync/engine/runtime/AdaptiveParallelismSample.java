package io.astrasync.engine.runtime;

/** One completed task latency and remaining split backlog observation. */
public record AdaptiveParallelismSample(long taskElapsedNanos, int queuedTasks, int activeParallelism) {
    public AdaptiveParallelismSample {
        if (taskElapsedNanos < 0) {
            throw new IllegalArgumentException("taskElapsedNanos must not be negative");
        }
        if (queuedTasks < 0) {
            throw new IllegalArgumentException("queuedTasks must not be negative");
        }
        if (activeParallelism <= 0) {
            throw new IllegalArgumentException("activeParallelism must be positive");
        }
    }
}
