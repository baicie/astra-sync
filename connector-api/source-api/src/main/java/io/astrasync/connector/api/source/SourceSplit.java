package io.astrasync.connector.api.source;

import io.astrasync.connector.api.SerializableState;

public interface SourceSplit extends SerializableState {

    String getSplitId();

    String getTableId();

    SourcePosition getPosition();

    SourcePosition getEndPosition();

    boolean isSnapshotSplit();

    int getParallelism();
}
