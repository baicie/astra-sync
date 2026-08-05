package io.astrasync.engine.plan;

import io.astrasync.engine.jobspec.DeliveryGuarantee;
import java.util.Objects;

public record CompiledJobPlan(
        String apiVersion,
        String jobName,
        ConnectorPlan source,
        ConnectorPlan sink,
        ExecutionMode executionMode,
        DeliveryGuarantee deliveryGuarantee,
        int maxBatchRecords) {
    public CompiledJobPlan {
        apiVersion = requireText(apiVersion, "apiVersion");
        jobName = requireText(jobName, "jobName");
        source = Objects.requireNonNull(source, "source must not be null");
        sink = Objects.requireNonNull(sink, "sink must not be null");
        executionMode = Objects.requireNonNull(executionMode, "executionMode must not be null");
        deliveryGuarantee = Objects.requireNonNull(deliveryGuarantee, "deliveryGuarantee must not be null");
        if (maxBatchRecords <= 0) {
            throw new IllegalArgumentException("maxBatchRecords must be positive");
        }
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
