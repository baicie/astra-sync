package io.astrasync.engine.runtime;

import java.util.function.BooleanSupplier;

/** Worker contract for an unbounded CDC task that emits durable progress barriers. */
public interface CheckpointCdcWorker {
    String workerId();

    WorkerResult executeCdc(
            CheckpointExecutionContext context,
            CdcTask task,
            CheckpointProgressListener progressListener,
            BooleanSupplier stopRequested);
}
