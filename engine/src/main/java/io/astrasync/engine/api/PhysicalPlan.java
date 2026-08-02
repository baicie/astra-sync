package io.astrasync.engine.api;

public interface PhysicalPlan {

    String getJobId();

    long getJobVersion();

    long getEpoch();

    ExecutionGraph getExecutionGraph();

    ResourceProfile getResourceProfile();
}

public interface ExecutionGraph {

    String getJobId();

    ExecutionVertex[] getVertices();

    ExecutionEdge[] getEdges();
}

public interface ExecutionVertex {

    String getVertexId();

    String getOperatorId();

    int getParallelism();

    ResourceProfile getResourceProfile();

    String getWorkerId();

    String getSlotId();
}

public interface ExecutionEdge {

    String getEdgeId();

    String getSourceVertexId();

    int getSourceOutputIndex();

    String getTargetVertexId();

    int getTargetInputIndex();

    ExchangeMode getExchangeMode();
}

public enum ExchangeMode {
    PIPELINE,
    BLOCKING,
    BROADCAST,
    HASH,
    RANGE
}

public interface ResourceProfile {

    double getCpuCores();

    long getHeapMemoryBytes();

    long getOffHeapMemoryBytes();

    long getNetworkMemoryBytes();

    long getDiskBytes();
}
