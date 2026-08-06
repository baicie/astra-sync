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
        return compile(jobSpec, false);
    }

    /** Compiles the distributed checkpoint runtime while keeping local Phase 1 callers unchanged. */
    public CompiledJobPlan compileCheckpointed(JobSpec jobSpec) {
        return compile(jobSpec, true);
    }

    private CompiledJobPlan compile(JobSpec jobSpec, boolean checkpointRuntime) {
        Objects.requireNonNull(jobSpec, "jobSpec must not be null");

        ConnectorDescriptor sourceDescriptor = resolve(jobSpec.spec().source(), ConnectorRole.SOURCE);
        ConnectorDescriptor sinkDescriptor = resolve(jobSpec.spec().sink(), ConnectorRole.SINK);

        requireRole(sourceDescriptor, ConnectorRole.SOURCE);
        requireRole(sinkDescriptor, ConnectorRole.SINK);
        boolean cdc = sourceDescriptor.hasCapability(Capability.CHANGE_DATA_CAPTURE);
        if (cdc) {
            if (!checkpointRuntime) {
                throw new JobCompilationException(
                        CompilationErrorCode.DELIVERY_UNSUPPORTED, "CDC sources require the checkpoint runtime");
            }
            requireCapability(sourceDescriptor, Capability.STREAM_READ, ConnectorRole.SOURCE);
            requireCapability(sourceDescriptor, Capability.REPLAYABLE_OFFSET, ConnectorRole.SOURCE);
            requireCapability(sinkDescriptor, Capability.UPSERT, ConnectorRole.SINK);
            requireCapability(sinkDescriptor, Capability.DELETE, ConnectorRole.SINK);
        } else {
            requireCapability(sourceDescriptor, Capability.BATCH_READ, ConnectorRole.SOURCE);
        }
        requireCapability(sinkDescriptor, Capability.BATCH_WRITE, ConnectorRole.SINK);

        if (!jobSpec.spec().transforms().isEmpty()) {
            throw new JobCompilationException(
                    CompilationErrorCode.TRANSFORM_UNSUPPORTED,
                    "Phase 0 does not execute transforms; remove $.spec.transforms entries");
        }

        DeliveryGuarantee requested = jobSpec.spec().delivery().guarantee();
        if (requested == DeliveryGuarantee.EXACTLY_ONCE) {
            if (!checkpointRuntime) {
                throw new JobCompilationException(
                        CompilationErrorCode.DELIVERY_UNSUPPORTED,
                        "requested exactly-once but the Phase 0 runtime supports only at-most-once because checkpoint, replay, and commit coordination are absent");
            }
            requireReplayableSource(jobSpec, sourceDescriptor, cdc);
            if (cdc) {
                requireCapability(sourceDescriptor, Capability.EXACTLY_ONCE_SOURCE, ConnectorRole.SOURCE);
            }
            if (!sinkDescriptor.hasCapability(Capability.TRANSACTIONAL_COMMIT)
                    && !sinkDescriptor.hasCapability(Capability.IDEMPOTENT_WRITE)) {
                throw new JobCompilationException(
                        CompilationErrorCode.DELIVERY_UNSUPPORTED,
                        "requested exactly-once but the sink lacks a transactional or idempotent commit capability");
            }
        }
        if (requested == DeliveryGuarantee.AT_LEAST_ONCE) {
            if (!checkpointRuntime) {
                throw new JobCompilationException(
                        CompilationErrorCode.DELIVERY_UNSUPPORTED,
                        "requested at-least-once but this runtime does not coordinate durable checkpoints");
            }
            requireReplayableSource(jobSpec, sourceDescriptor, cdc);
        }

        return new CompiledJobPlan(
                jobSpec.apiVersion(),
                jobSpec.metadata().name(),
                connectorPlan(
                        ConnectorRole.SOURCE, sourceDescriptor, jobSpec.spec().source()),
                connectorPlan(ConnectorRole.SINK, sinkDescriptor, jobSpec.spec().sink()),
                cdc ? ExecutionMode.CDC : ExecutionMode.BATCH,
                requested,
                jobSpec.spec().runtime().maxBatchRecords(),
                jobSpec.spec().runtime().adaptiveBatch(),
                jobSpec.spec().runtime().adaptiveParallelism());
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

    private static void requireReplayableSource(JobSpec jobSpec, ConnectorDescriptor sourceDescriptor, boolean cdc) {
        requireCapability(sourceDescriptor, Capability.REPLAYABLE_OFFSET, ConnectorRole.SOURCE);
        if (cdc) {
            return;
        }
        String resumeColumn = jobSpec.spec().source().options().get("resumeColumn");
        if (resumeColumn == null || resumeColumn.isBlank()) {
            throw new JobCompilationException(
                    CompilationErrorCode.DELIVERY_UNSUPPORTED,
                    jobSpec.spec().delivery().guarantee().externalName()
                            + " requires an explicit stable unique source resumeColumn");
        }
    }

    private static ConnectorPlan connectorPlan(ConnectorRole role, ConnectorDescriptor descriptor, ConnectorSpec spec) {
        return new ConnectorPlan(role, descriptor.name(), descriptor.version(), spec.options());
    }
}
