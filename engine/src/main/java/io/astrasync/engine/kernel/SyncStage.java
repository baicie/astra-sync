package io.astrasync.engine.kernel;

public enum SyncStage {
    CANCELLED,
    SOURCE_OPEN,
    SINK_OPEN,
    SOURCE_READ,
    TRANSFORM,
    SINK_WRITE,
    CLOSE
}
