package io.astrasync.engine.coordinator;

import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.ConnectorFactory;
import io.astrasync.connector.api.sink.CdcSink;
import io.astrasync.connector.api.source.CdcSource;
import io.astrasync.engine.checkpoint.CheckpointStore;
import io.astrasync.engine.jobspec.DeliveryGuarantee;
import io.astrasync.engine.jobspec.JobSpec;
import io.astrasync.engine.plan.CompiledJobPlan;
import io.astrasync.engine.plan.ConnectorPlan;
import io.astrasync.engine.plan.ConnectorRegistry;
import io.astrasync.engine.plan.ExecutionMode;
import io.astrasync.engine.plan.JobCompiler;
import io.astrasync.engine.runtime.CdcTask;
import io.astrasync.engine.runtime.CheckpointCdcWorker;
import java.nio.charset.StandardCharsets;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.time.Duration;
import java.util.Objects;
import java.util.function.BooleanSupplier;

/** Compiles, materializes, and runs one checkpointed CDC job in the current process. */
public final class LocalCdcJobRunner {
    private static final String TASK_ID = "cdc-0";

    private final ConnectorRegistry registry;
    private final CheckpointStore checkpointStore;
    private final CheckpointCdcWorker worker;
    private final Duration pollTimeout;

    public LocalCdcJobRunner(
            ConnectorRegistry registry,
            CheckpointStore checkpointStore,
            CheckpointCdcWorker worker,
            Duration pollTimeout) {
        this.registry = Objects.requireNonNull(registry, "registry must not be null");
        this.checkpointStore = Objects.requireNonNull(checkpointStore, "checkpointStore must not be null");
        this.worker = Objects.requireNonNull(worker, "worker must not be null");
        this.pollTimeout = Objects.requireNonNull(pollTimeout, "pollTimeout must not be null");
        if (pollTimeout.isZero() || pollTimeout.isNegative()) {
            throw new IllegalArgumentException("pollTimeout must be positive");
        }
    }

    public CdcJobRunResult run(JobSpec jobSpec, BooleanSupplier stopRequested) {
        return run(jobSpec, stopRequested, 0);
    }

    public CdcJobRunResult run(JobSpec jobSpec, BooleanSupplier stopRequested, long maxCheckpoints) {
        CompiledJobPlan plan = new JobCompiler(registry)
                .compileCheckpointed(Objects.requireNonNull(jobSpec, "jobSpec must not be null"));
        if (plan.executionMode() != ExecutionMode.CDC) {
            throw new IllegalArgumentException("local CDC runner requires a CDC source connector");
        }
        if (plan.deliveryGuarantee() != DeliveryGuarantee.EXACTLY_ONCE) {
            throw new IllegalArgumentException("local CDC runner requires exactly-once delivery");
        }

        CdcSource source = createSource(plan.source());
        CdcSink sink;
        try {
            sink = createSink(plan.sink());
        } catch (RuntimeException exception) {
            try {
                source.close();
            } catch (RuntimeException closeFailure) {
                exception.addSuppressed(closeFailure);
            }
            throw exception;
        }
        CdcTask task = new CdcTask(TASK_ID, source, sink, pollTimeout);
        CdcRunResult result = new CheckpointCdcCoordinator(worker, checkpointStore)
                .run(
                        plan.jobName(),
                        sourceIdentity(plan.source()),
                        task,
                        Objects.requireNonNull(stopRequested, "stopRequested must not be null"),
                        maxCheckpoints);
        return new CdcJobRunResult(plan, result);
    }

    private CdcSource createSource(ConnectorPlan connectorPlan) {
        ConnectorFactory factory = registry.requireFactory(connectorPlan.connector(), connectorPlan.version());
        return Objects.requireNonNull(
                factory.createCdcSource(ConnectorConfiguration.of(connectorPlan.options())),
                "connector factory returned a null CDC source");
    }

    private CdcSink createSink(ConnectorPlan connectorPlan) {
        ConnectorFactory factory = registry.requireFactory(connectorPlan.connector(), connectorPlan.version());
        return Objects.requireNonNull(
                factory.createCdcSink(ConnectorConfiguration.of(connectorPlan.options())),
                "connector factory returned a null CDC sink");
    }

    private static String sourceIdentity(ConnectorPlan source) {
        String canonical = source.connector() + '|' + source.version() + '|' + source.options();
        try {
            byte[] bytes = MessageDigest.getInstance("SHA-256").digest(canonical.getBytes(StandardCharsets.UTF_8));
            StringBuilder digest = new StringBuilder(bytes.length * 2);
            for (byte item : bytes) {
                digest.append(Character.forDigit((item >>> 4) & 0xf, 16));
                digest.append(Character.forDigit(item & 0xf, 16));
            }
            return source.connector() + ':' + digest;
        } catch (NoSuchAlgorithmException exception) {
            throw new IllegalStateException("SHA-256 is not available", exception);
        }
    }
}
