package io.astrasync.engine.runtime;

import io.astrasync.connector.api.source.SourceSplit;

/** Materializes a resource-owned task for one enumerated split. */
@FunctionalInterface
public interface BatchTaskFactory {
    BatchTask create(SourceSplit split);

    /**
     * Materializes a checkpoint-aware task when the factory supports one. Existing Phase 1
     * factories retain their unary behavior through this default.
     */
    default BatchTask create(SourceSplit split, CheckpointExecutionContext context) {
        return create(split);
    }
}
