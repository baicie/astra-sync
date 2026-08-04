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
import io.astrasync.engine.runtime.BatchWorker;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.util.List;
import java.util.Objects;

/** Executable Coordinator for the operational JDBC full-load path. */
public final class CoordinatorApplication {
    private CoordinatorApplication() {}

    public static void main(String[] args) {
        try {
            ResumableRunResult result = run(CoordinatorConfiguration.fromEnvironment(System.getenv()));
            System.out.printf(
                    "SUCCEEDED resumedSplits=%d executedSplits=%d recordsRead=%d recordsWritten=%d%n",
                    result.resumedSplitCount(),
                    result.executedSplitCount(),
                    result.metrics().readCount(),
                    result.metrics().writtenCount());
        } catch (RuntimeException exception) {
            System.err.println("FAILED to start or execute Coordinator: " + message(exception));
            System.exit(1);
        }
    }

    public static ResumableRunResult run(CoordinatorConfiguration configuration) {
        CoordinatorConfiguration checked = Objects.requireNonNull(configuration, "configuration must not be null");
        JobSpec jobSpec = readJobSpec(checked);
        CompiledJobPlan plan = new JobCompiler(ConnectorRegistry.of(new JdbcConnectorFactory())).compile(jobSpec);
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
        return new ResumableBatchCoordinator(workers, new FileSplitProgressStore(checked.progressDirectory()))
                .run(
                        jobSpec.metadata().name(),
                        splitSource,
                        new RemoteTaskFactory(plan.maxBatchRecords(), checked.maxInFlightBatches()));
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
