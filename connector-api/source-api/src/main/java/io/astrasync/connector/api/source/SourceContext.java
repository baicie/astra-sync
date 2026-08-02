package io.astrasync.connector.api.source;

public interface SourceContext {

    String getJobId();

    String getTaskId();

    SourcePosition getStartingPosition();

    SplitDiscoveryMode getDiscoveryMode();

    SourceConfig getConfig();
}

public enum SplitDiscoveryMode {
    SNAPSHOT,
    CDC,
    SNAPSHOT_THEN_CDC,
    INCREMENTAL
}
