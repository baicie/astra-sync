package io.astrasync.engine.runtime;

import io.astrasync.connector.api.source.SourceSplit;

/** Materializes a resource-owned task for one enumerated split. */
@FunctionalInterface
public interface BatchTaskFactory {
    BatchTask create(SourceSplit split);
}
