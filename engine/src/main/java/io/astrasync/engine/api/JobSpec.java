package io.astrasync.engine.api;

import java.util.Map;

public interface JobSpec {

    String getApiVersion();

    String getKind();

    JobMetadata getMetadata();

    JobConfiguration getSpec();
}

interface JobMetadata {

    String getName();

    String getNamespace();

    String getUid();

    long getVersion();

    long getCreationTimestamp();
}

interface JobConfiguration {

    SourceConfig getSource();

    SinkConfig getSink();

    TransformConfig[] getTransforms();

    DeliveryConfig getDelivery();

    ParallelismConfig getParallelism();

    CheckpointConfig getCheckpoint();

    RetryConfig getRetry();
}

interface SourceConfig {

    String getConnector();

    String getConnectionRef();

    TableSelector getTables();
}

interface TableSelector {
    String[] getInclude();

    String[] getExclude();
}

interface SinkConfig {

    String getConnector();

    String getConnectionRef();

    String getTargetTable();
}

interface TransformConfig {

    String getType();

    Map<String, String> getOptions();
}

interface DeliveryConfig {

    DeliveryGuarantee getGuarantee();
}

enum DeliveryGuarantee {
    EXACTLY_ONCE,
    AT_LEAST_ONCE,
    AT_MOST_ONCE
}

interface ParallelismConfig {

    int getInitial();

    int getMin();

    int getMax();
}

interface CheckpointConfig {

    long getIntervalMillis();

    long getTimeoutMillis();

    int getMaxRetained();
}

interface RetryConfig {

    int getMaxAttempts();

    long getBackoffMillis();

    double getBackoffMultiplier();
}
