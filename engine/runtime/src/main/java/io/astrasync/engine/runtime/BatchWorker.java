package io.astrasync.engine.runtime;

/** Executes one assigned task and returns its task-local metrics. */
public interface BatchWorker {
    String workerId();

    WorkerResult execute(BatchTask task);
}
