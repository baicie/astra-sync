package io.astrasync.engine.runtime;

/** Measurements from one completed source/sink batch. */
public record AdaptiveBatchSample(
        int records, long processingNanos, long queueWaitNanos, int queueDepth, int queueCapacity) {
    public AdaptiveBatchSample {
        if (records < 0) {
            throw new IllegalArgumentException("records must not be negative");
        }
        if (processingNanos < 0) {
            throw new IllegalArgumentException("processingNanos must not be negative");
        }
        if (queueWaitNanos < 0) {
            throw new IllegalArgumentException("queueWaitNanos must not be negative");
        }
        if (queueDepth < 0 || queueCapacity <= 0 || queueDepth > queueCapacity) {
            throw new IllegalArgumentException("queue depth must be within queue capacity");
        }
    }
}
