package io.astrasync.engine.runtime;

import java.util.Objects;
import java.util.concurrent.ConcurrentHashMap;
import java.util.concurrent.ConcurrentMap;

/** Monotonic process-local fence used by Workers to reject stale task executions. */
public final class EpochFence {
    private final ConcurrentMap<String, Long> activeEpochs = new ConcurrentHashMap<>();

    public void activate(String jobId, long executionEpoch) {
        String checkedJobId = requireText(jobId, "jobId");
        if (executionEpoch <= 0) {
            throw new IllegalArgumentException("executionEpoch must be positive");
        }
        activeEpochs.compute(checkedJobId, (ignored, currentValue) -> {
            long current = currentValue == null ? 0 : currentValue;
            if (executionEpoch < current) {
                throw new EpochFencedException(
                        "execution epoch " + executionEpoch + " is stale; active epoch is " + current);
            }
            return executionEpoch;
        });
    }

    public void assertCurrent(String jobId, long executionEpoch) {
        String checkedJobId = requireText(jobId, "jobId");
        if (!Objects.equals(activeEpochs.get(checkedJobId), executionEpoch)) {
            throw new EpochFencedException(
                    "execution epoch " + executionEpoch + " is no longer active for job " + jobId);
        }
    }

    public long activeEpoch() {
        return activeEpochs.values().stream().mapToLong(Long::longValue).max().orElse(0);
    }

    public long activeEpoch(String jobId) {
        return activeEpochs.getOrDefault(requireText(jobId, "jobId"), 0L);
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
