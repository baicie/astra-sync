package io.astrasync.engine.kernel;

public enum SyncStage {
    CANCELLED,
    CANCELLATION_CHECK,
    SOURCE_OPEN,
    SINK_OPEN,
    SOURCE_READ,
    TRANSFORM,
    SINK_WRITE,
    CLOSE
}
