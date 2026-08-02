package io.astrasync.connector.api.source;

import io.astrasync.connector.api.SerializableState;

public interface ReaderState extends SerializableState {

    long getCheckpointId();

    SourcePosition getPosition();

    int getReaderId();

    int getSplitCount();
}
