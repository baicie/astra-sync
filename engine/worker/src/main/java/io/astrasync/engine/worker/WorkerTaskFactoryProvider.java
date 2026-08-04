package io.astrasync.engine.worker;

import io.astrasync.engine.runtime.BatchTaskFactory;
import java.util.Map;

/** Deployment extension that materializes Worker-local task resources from process configuration. */
@FunctionalInterface
public interface WorkerTaskFactoryProvider {
    BatchTaskFactory create(Map<String, String> environment);
}
