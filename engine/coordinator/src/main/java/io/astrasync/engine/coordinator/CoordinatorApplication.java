package io.astrasync.engine.coordinator;

import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.source.SplitSource;
import io.astrasync.connector.jdbc.JdbcConnectorFactory;
import io.astrasync.connector.jdbc.JdbcRangeSplitSource;
import io.astrasync.engine.checkpoint.FileSplitProgressStore;
import io.astrasync.engine.jobspec.JobSpec;
import io.astrasync.engine.jobspec.JobSpecParser;
import io.astrasync.engine.network.RemoteBatchWorker;
import io.astrasync.engine.network.RemoteTaskFactory;
import io.astrasync.engine.network.WorkerClient;
import io.astrasync.engine.plan.CompiledJobPlan;
import io.astrasync.engine.plan.ConnectorRegistry;
import io.astrasync.engine.plan.JobCompiler;
import io.astrasync.engine.runtime.AdaptiveBatchPolicy;
import io.astrasync.engine.runtime.AdaptiveParallelismPolicy;
import io.astrasync.engine.runtime.BatchWorker;
import io.astrasync.engine.runtime.RuntimeCredentialLoader;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import org.slf4j.Logger;
import org.slf4j.LoggerFactory;

/** Executable Coordinator for the operational JDBC full-load path. */
public final class CoordinatorApplication {
    private static final Logger LOG = LoggerFactory.getLogger(CoordinatorApplication.class);

    private CoordinatorApplication() {}

    public static void main(String[] args) {
        try {
            Map<String, String> environment = System.getenv();
            ResumableRunResult result = run(CoordinatorConfiguration.fromEnvironment(environment), environment);
            if (result.executionEpoch() == 0) {
                System.out.printf(
                        "SUCCEEDED resumedSplits=%d executedSplits=%d recordsRead=%d recordsWritten=%d%n",
                        result.resumedSplitCount(),
                        result.executedSplitCount(),
                        result.metrics().readCount(),
                        result.metrics().writtenCount());
            } else {
                System.out.printf(
                        "SUCCEEDED executionEpoch=%d resumedSplits=%d recoveredSplits=%d executedSplits=%d recordsRead=%d recordsWritten=%d%n",
                        result.executionEpoch(),
                        result.resumedSplitCount(),
                        result.recoveredSplitCount(),
                        result.executedSplitCount(),
                        result.metrics().readCount(),
                        result.metrics().writtenCount());
            }
        } catch (RuntimeException exception) {
            logFailure(exception);
            System.err.println("FAILED to start or execute Coordinator: " + message(exception));
            System.exit(1);
        }
    }

    static void logFailure(RuntimeException exception) {
        LOG.error("coordinator failed to start or execute", exception);
    }

    @SuppressWarnings("try")
    public static ResumableRunResult run(CoordinatorConfiguration configuration) {
        return run(configuration, Map.of());
    }

    static ResumableRunResult run(CoordinatorConfiguration configuration, Map<String, String> environment) {
        CoordinatorConfiguration checked = Objects.requireNonNull(configuration, "configuration must not be null");
        try (ExecutionHeartbeat ignored = ExecutionHeartbeat.start(checked.heartbeat())) {
            return runChecked(checked, Objects.requireNonNull(environment, "environment must not be null"));
        }
    }

    private static ResumableRunResult runChecked(CoordinatorConfiguration checked, Map<String, String> environment) {
        ConnectorRegistry registry = ConnectorRegistry.of(new JdbcConnectorFactory());
        JobSpec jobSpec = RuntimeCredentialLoader.load(readJobSpec(checked), registry, environment);
        CompiledJobPlan plan = new JobCompiler(registry).compileCheckpointed(jobSpec);
        requireJdbcPlan(plan);

        SplitSource splitSource =
                new JdbcRangeSplitSource(ConnectorConfiguration.of(plan.source().options()));
        List<BatchWorker> workers = checked.workers().stream()
                .map(endpoint -> new RemoteBatchWorker(
                        endpoint.workerId(),
                        new WorkerClient(endpoint.host(), endpoint.port(), checked.workerTimeout()),
                        checked.maxInFlightTasks()))
                .map(worker -> (BatchWorker) worker)
                .toList();
        String jobId = jobSpec.metadata().name();
        boolean checkpointExecution =
                plan.deliveryGuarantee() == io.astrasync.engine.jobspec.DeliveryGuarantee.AT_LEAST_ONCE
                        || plan.deliveryGuarantee() == io.astrasync.engine.jobspec.DeliveryGuarantee.EXACTLY_ONCE;
        RemoteTaskFactory taskFactory = new RemoteTaskFactory(
                plan.maxBatchRecords(),
                checked.maxInFlightBatches(),
                plan.deliveryGuarantee() == io.astrasync.engine.jobspec.DeliveryGuarantee.EXACTLY_ONCE,
                new AdaptiveBatchPolicy(
                        plan.adaptiveBatch().minBatchRecords(),
                        plan.adaptiveBatch().initialBatchRecords(),
                        plan.adaptiveBatch().targetBatchNanos(),
                        plan.adaptiveBatch().adjustmentCooldownSamples()),
                plan.spill());
        AdaptiveParallelismPolicy parallelismPolicy = plan.adaptiveParallelism().enabled()
                ? new AdaptiveParallelismPolicy(
                        plan.adaptiveParallelism().minParallelism(),
                        plan.adaptiveParallelism().initialParallelism(),
                        plan.adaptiveParallelism().maxParallelism(),
                        plan.adaptiveParallelism().targetTaskNanos(),
                        plan.adaptiveParallelism().adjustmentCooldownSamples())
                : null;
        if (checkpointExecution) {
            CheckpointRunResult checkpointed = new CheckpointBatchCoordinator(
                            workers,
                            new io.astrasync.engine.checkpoint.FileCheckpointStore(checked.progressDirectory()))
                    .run(jobId, splitSource, taskFactory, checked.executionEpoch());
            return new ResumableRunResult(
                    checkpointed.taskResults(),
                    checkpointed.metrics(),
                    checkpointed.resumedSplitCount(),
                    checkpointed.executedSplitCount(),
                    checkpointed.executionEpoch(),
                    checkpointed.recoveredSplitCount());
        }
        return new ResumableBatchCoordinator(workers, new FileSplitProgressStore(checked.progressDirectory()))
                .run(jobId, splitSource, taskFactory, parallelismPolicy);
    }

    private static JobSpec readJobSpec(CoordinatorConfiguration configuration) {
        String document;
        try {
            document = Files.readString(configuration.jobSpecPath(), StandardCharsets.UTF_8);
        } catch (IOException exception) {
            throw new IllegalArgumentException("failed to read Coordinator JobSpec", exception);
        }
        return new JobSpecParser().parse(document);
    }

    private static void requireJdbcPlan(CompiledJobPlan plan) {
        if (!JdbcConnectorFactory.CONNECTOR_NAME.equals(plan.source().connector())
                || !JdbcConnectorFactory.CONNECTOR_NAME.equals(plan.sink().connector())) {
            throw new IllegalArgumentException("operational Coordinator currently supports only JDBC source and sink");
        }
    }

    private static String message(RuntimeException exception) {
        return exception.getMessage() == null ? exception.getClass().getSimpleName() : exception.getMessage();
    }
}
