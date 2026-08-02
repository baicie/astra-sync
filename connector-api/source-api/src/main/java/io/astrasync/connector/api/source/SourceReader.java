package io.astrasync.connector.api.source;

import io.astrasync.connector.api.*;
import io.astrasync.connector.api.data.RecordBatch;

import java.util.Collection;

public interface SourceReader extends AutoCloseable {

    void open();

    PollResult<RecordBatch> pollNext();

    ReaderState snapshotState(long checkpointId);

    void addSplits(Collection<SourceSplit> splits);

    void close();
}
