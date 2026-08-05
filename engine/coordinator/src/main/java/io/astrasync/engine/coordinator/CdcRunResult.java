package io.astrasync.engine.coordinator;

import io.astrasync.engine.runtime.WorkerResult;
import java.util.Objects;

/** Result returned when a CDC run is deliberately stopped. */
public record CdcRunResult(WorkerResult workerResult, long executionEpoch, long checkpointSequence, boolean recovered) {
    public CdcRunResult {
        workerResult = Objects.requireNonNull(workerResult, "workerResult must not be null");
        if (executionEpoch <= 0 || checkpointSequence < 0) {
            throw new IllegalArgumentException("executionEpoch must be positive and checkpointSequence non-negative");
        }
    }
}
