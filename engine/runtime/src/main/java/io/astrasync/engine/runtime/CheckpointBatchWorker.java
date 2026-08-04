package io.astrasync.engine.runtime;

/** Optional Worker execution contract that pauses after each committed checkpoint barrier. */
public interface CheckpointBatchWorker {
    WorkerResult executeCheckpoint(
            CheckpointExecutionContext context, BatchTask task, CheckpointProgressListener progressListener);
}
