package io.astrasync.engine.plan;

import io.astrasync.connector.api.Capability;
import io.astrasync.connector.api.ConnectorDescriptor;
import io.astrasync.connector.api.ConnectorRole;
import io.astrasync.engine.jobspec.ConnectorSpec;
import io.astrasync.engine.jobspec.DeliveryGuarantee;
import io.astrasync.engine.jobspec.JobSpec;
import java.util.Objects;

public final class JobCompiler {
    private final ConnectorRegistry registry;

    public JobCompiler(ConnectorRegistry registry) {
        this.registry = Objects.requireNonNull(registry, "registry must not be null");
    }

    public CompiledJobPlan compile(JobSpec jobSpec) {
        Objects.requireNonNull(jobSpec, "jobSpec must not be null");

        ConnectorDescriptor sourceDescriptor = resolve(jobSpec.spec().source(), ConnectorRole.SOURCE);
        ConnectorDescriptor sinkDescriptor = resolve(jobSpec.spec().sink(), ConnectorRole.SINK);

        requireRole(sourceDescriptor, ConnectorRole.SOURCE);
        requireRole(sinkDescriptor, ConnectorRole.SINK);
        requireCapability(sourceDescriptor, Capability.BATCH_READ, ConnectorRole.SOURCE);
        requireCapability(sinkDescriptor, Capability.BATCH_WRITE, ConnectorRole.SINK);

        if (!jobSpec.spec().transforms().isEmpty()) {
            throw new JobCompilationException(
                    CompilationErrorCode.TRANSFORM_UNSUPPORTED,
                    "Phase 0 does not execute transforms; remove $.spec.transforms entries");
        }

        DeliveryGuarantee requested = jobSpec.spec().delivery().guarantee();
        if (requested != DeliveryGuarantee.AT_MOST_ONCE) {
            throw new JobCompilationException(
                    CompilationErrorCode.DELIVERY_UNSUPPORTED,
                    "requested " + requested.externalName()
                            + " but the Phase 0 runtime supports only at-most-once because checkpoint, replay, and commit coordination are absent");
        }

        return new CompiledJobPlan(
                jobSpec.apiVersion(),
                jobSpec.metadata().name(),
                connectorPlan(
                        ConnectorRole.SOURCE, sourceDescriptor, jobSpec.spec().source()),
                connectorPlan(ConnectorRole.SINK, sinkDescriptor, jobSpec.spec().sink()),
                DeliveryGuarantee.AT_MOST_ONCE,
                jobSpec.spec().runtime().maxBatchRecords());
    }

    private ConnectorDescriptor resolve(ConnectorSpec connectorSpec, ConnectorRole role) {
        return registry.findDescriptor(connectorSpec.connector())
                .orElseThrow(() -> new JobCompilationException(
                        CompilationErrorCode.CONNECTOR_NOT_FOUND,
                        role + " connector is not registered: " + connectorSpec.connector()));
    }

    private static void requireRole(ConnectorDescriptor descriptor, ConnectorRole role) {
        if (!descriptor.supportsRole(role)) {
            throw new JobCompilationException(
                    CompilationErrorCode.ROLE_UNSUPPORTED,
                    "connector " + descriptor.name() + " does not support role " + role);
        }
    }

    private static void requireCapability(ConnectorDescriptor descriptor, Capability capability, ConnectorRole role) {
        if (!descriptor.hasCapability(capability)) {
            throw new JobCompilationException(
                    CompilationErrorCode.CAPABILITY_MISSING,
                    role + " connector " + descriptor.name() + " lacks required capability " + capability);
        }
    }

    private static ConnectorPlan connectorPlan(ConnectorRole role, ConnectorDescriptor descriptor, ConnectorSpec spec) {
        return new ConnectorPlan(role, descriptor.name(), descriptor.version(), spec.options());
    }
}
