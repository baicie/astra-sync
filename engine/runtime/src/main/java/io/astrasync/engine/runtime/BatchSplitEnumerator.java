package io.astrasync.engine.runtime;

import java.util.List;

/** Enumerates independent, resource-owned batch tasks for a Coordinator. */
@FunctionalInterface
public interface BatchSplitEnumerator {
    List<BatchTask> enumerate();
}
