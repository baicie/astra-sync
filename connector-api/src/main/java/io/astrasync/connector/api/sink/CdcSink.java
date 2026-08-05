package io.astrasync.connector.api.sink;

import io.astrasync.connector.api.CheckpointContext;
import io.astrasync.connector.api.SinkCommitContext;
import io.astrasync.connector.api.data.CdcBatch;

/** Checkpoint-aware sink for ordered inserts, updates, deletes, and snapshot rows. */
public interface CdcSink extends AutoCloseable {
    void open(CheckpointContext context);

    void writeBatch(CdcBatch batch, SinkCommitContext commitContext);

    String lastCommitToken();

    @Override
    void close();
}
