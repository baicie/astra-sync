package io.astrasync.connector.api.source;

import io.astrasync.connector.api.SerializableState;

import java.util.Map;

public interface EnumeratorState extends SerializableState {

    long getCheckpointId();

    Map<Integer, SourcePosition> getReaderPositions();

    Map<Integer, SourceSplit> getAssignedSplits();
}
