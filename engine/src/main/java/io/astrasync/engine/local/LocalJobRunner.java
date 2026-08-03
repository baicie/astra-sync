package io.astrasync.engine.local;

import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.ConnectorFactory;
import io.astrasync.connector.api.ConnectorRole;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.engine.jobspec.JobSpec;
import io.astrasync.engine.kernel.CancellationToken;
import io.astrasync.engine.kernel.SingleNodeSyncJob;
import io.astrasync.engine.kernel.SyncJobException;
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.kernel.SyncStage;
import io.astrasync.engine.plan.CompiledJobPlan;
import io.astrasync.engine.plan.ConnectorPlan;
import io.astrasync.engine.plan.ConnectorRegistry;
import io.astrasync.engine.plan.JobCompiler;
import java.util.Objects;

/** Compiles, materializes, and executes one job in the current process. */
public final class LocalJobRunner {
    private final ConnectorRegistry registry;
    private final JobCompiler compiler;

    public LocalJobRunner(ConnectorRegistry registry) {
        this.registry = Objects.requireNonNull(registry, "registry must not be null");
        this.compiler = new JobCompiler(registry);
    }

    public LocalRunResult run(JobSpec jobSpec) {
        return run(jobSpec, CancellationToken.neverCancelled());
    }

    public LocalRunResult run(JobSpec jobSpec, CancellationToken cancellationToken) {
        CompiledJobPlan plan = compiler.compile(Objects.requireNonNull(jobSpec, "jobSpec must not be null"));
        CancellationToken token = Objects.requireNonNull(cancellationToken, "cancellationToken must not be null");
        checkCancelled(token);

        BatchSource source = createSource(plan.source());
        checkCancelled(token);
        BatchSink sink = createSink(plan.sink());
        SyncResult metrics = SingleNodeSyncJob.builder()
                .source(source)
                .sink(sink)
                .maxBatchRecords(plan.maxBatchRecords())
                .cancellationToken(token)
                .build()
                .run();
        return new LocalRunResult(plan, metrics);
    }

    private static void checkCancelled(CancellationToken cancellationToken) {
        if (cancellationToken.isCancelled()) {
            throw new SyncJobException(
                    SyncStage.CANCELLED,
                    "job cancelled before connector materialization",
                    null,
                    new SyncResult(0, 0, 0, 0, 0));
        }
    }

    private BatchSource createSource(ConnectorPlan connectorPlan) {
        requireRole(connectorPlan, ConnectorRole.SOURCE);
        ConnectorFactory factory = requireFactory(connectorPlan);
        try {
            return Objects.requireNonNull(
                    factory.createSource(ConnectorConfiguration.of(connectorPlan.options())),
                    "connector factory returned a null Source");
        } catch (RuntimeException exception) {
            throw materializationFailure(connectorPlan, exception);
        }
    }

    private BatchSink createSink(ConnectorPlan connectorPlan) {
        requireRole(connectorPlan, ConnectorRole.SINK);
        ConnectorFactory factory = requireFactory(connectorPlan);
        try {
            return Objects.requireNonNull(
                    factory.createSink(ConnectorConfiguration.of(connectorPlan.options())),
                    "connector factory returned a null Sink");
        } catch (RuntimeException exception) {
            throw materializationFailure(connectorPlan, exception);
        }
    }

    private ConnectorFactory requireFactory(ConnectorPlan connectorPlan) {
        try {
            return registry.requireFactory(connectorPlan.connector(), connectorPlan.version());
        } catch (RuntimeException exception) {
            throw materializationFailure(connectorPlan, exception);
        }
    }

    private static void requireRole(ConnectorPlan connectorPlan, ConnectorRole expectedRole) {
        if (connectorPlan.role() != expectedRole) {
            throw new IllegalArgumentException(
                    "expected a " + expectedRole + " connector plan but received " + connectorPlan.role());
        }
    }

    private static JobMaterializationException materializationFailure(
            ConnectorPlan connectorPlan, RuntimeException cause) {
        return new JobMaterializationException(
                connectorPlan.role(),
                connectorPlan.connector(),
                "failed to materialize " + connectorPlan.role() + " connector '" + connectorPlan.connector() + "': "
                        + cause.getMessage(),
                cause);
    }
}
