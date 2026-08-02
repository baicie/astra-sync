package io.astrasync.connector.api.source;

import io.astrasync.connector.api.*;

import java.util.Collection;

public interface SplitEnumerator {

    Collection<SourceSplit> initialSplits();

    void addReader(int readerId);

    void addSplitsBack(Collection<SourceSplit> splits, int readerId);

    EnumeratorState snapshotState(long checkpointId);

    void close();
}
