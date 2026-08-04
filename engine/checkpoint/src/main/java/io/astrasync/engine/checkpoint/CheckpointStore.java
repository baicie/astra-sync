package io.astrasync.engine.checkpoint;

import java.util.Optional;

/** Durable checkpoint and execution-epoch contract. */
public interface CheckpointStore {
    long acquireEpoch(String jobId, SplitPlan plan);

    Optional<CheckpointRecord> load(String jobId, String splitId);

    Optional<CheckpointCompletion> loadCompletion(String jobId, String splitId);

    CheckpointRecord record(CheckpointRecord checkpoint);

    CheckpointCompletion recordCompletion(CheckpointCompletion completion);
}
