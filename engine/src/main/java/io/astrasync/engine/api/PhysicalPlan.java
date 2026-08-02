package io.astrasync.engine.api;

public interface PhysicalPlan {

    String getJobId();

    long getJobVersion();

    long getEpoch();

    ExecutionGraph getExecutionGraph();

    ResourceProfile getResourceProfile();
}

interface ExecutionGraph {

    String getJobId();

    ExecutionVertex[] getVertices();

    ExecutionEdge[] getEdges();
}

interface ExecutionVertex {

    String getVertexId();

    String getOperatorId();

    int getParallelism();

    ResourceProfile getResourceProfile();

    String getWorkerId();

    String getSlotId();
}

interface ExecutionEdge {

    String getEdgeId();

    String getSourceVertexId();

    int getSourceOutputIndex();

    String getTargetVertexId();

    int getTargetInputIndex();

    ExchangeMode getExchangeMode();
}

enum ExchangeMode {
    PIPELINE,
    BLOCKING,
    BROADCAST,
    HASH,
    RANGE
}

interface ResourceProfile {

    double getCpuCores();

    long getHeapMemoryBytes();

    long getOffHeapMemoryBytes();

    long getNetworkMemoryBytes();

    long getDiskBytes();
}
