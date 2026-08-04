package io.astrasync.engine.checkpoint;

import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.engine.runtime.WorkerResult;
import java.util.Optional;

/** Durable first-success store for resumable full-load splits. */
public interface SplitProgressStore {
    FullLoadProgress open(String jobId, SplitPlan plan);

    Optional<FullLoadProgress> load(String jobId);

    FullLoadProgress recordCompletion(String jobId, String planFingerprint, SourceSplit split, WorkerResult result);
}
