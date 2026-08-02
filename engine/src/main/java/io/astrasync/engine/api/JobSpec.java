package io.astrasync.engine.api;

public interface JobSpec {

    String getApiVersion();

    String getKind();

    JobMetadata getMetadata();

    JobConfiguration getSpec();
}

public interface JobMetadata {

    String getName();

    String getNamespace();

    String getUid();

    long getVersion();

    long getCreationTimestamp();
}

public interface JobConfiguration {

    SourceConfig getSource();

    SinkConfig getSink();

    TransformConfig[] getTransforms();

    DeliveryConfig getDelivery();

    ParallelismConfig getParallelism();

    CheckpointConfig getCheckpoint();

    RetryConfig getRetry();
}

public interface SourceConfig {

    String getConnector();

    String getConnectionRef();

    TableSelector getTables();
}

public interface SinkConfig {

    String getConnector();

    String getConnectionRef();

    String getTargetTable();
}

public interface TransformConfig {

    String getType();

    Map<String, String> getOptions();
}

public interface DeliveryConfig {

    DeliveryGuarantee getGuarantee();
}

public enum DeliveryGuarantee {
    EXACTLY_ONCE,
    AT_LEAST_ONCE,
    AT_MOST_ONCE
}

public interface ParallelismConfig {

    int getInitial();

    int getMin();

    int getMax();
}

public interface CheckpointConfig {

    long getIntervalMillis();

    long getTimeoutMillis();

    int getMaxRetained();
}

public interface RetryConfig {

    int getMaxAttempts();

    long getBackoffMillis();

    double getBackoffMultiplier();
}
