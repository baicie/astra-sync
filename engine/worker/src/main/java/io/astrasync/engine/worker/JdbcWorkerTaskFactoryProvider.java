package io.astrasync.engine.worker;

import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.connector.jdbc.JdbcConnectorFactory;
import io.astrasync.connector.jdbc.JdbcRangeSplitSource;
import io.astrasync.engine.jobspec.JobSpec;
import io.astrasync.engine.jobspec.JobSpecParser;
import io.astrasync.engine.plan.CompiledJobPlan;
import io.astrasync.engine.plan.ConnectorRegistry;
import io.astrasync.engine.plan.JobCompiler;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskFactory;
import io.astrasync.engine.runtime.CheckpointExecutionContext;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.Map;
import java.util.Objects;

/** Production JDBC task provider backed by the same immutable JobSpec used by the Coordinator. */
public final class JdbcWorkerTaskFactoryProvider implements WorkerTaskFactoryProvider {
    public static final String JOB_SPEC_ENVIRONMENT = "ASTRASYNC_WORKER_JOB_SPEC";
    public static final String MAX_IN_FLIGHT_BATCHES_ENVIRONMENT = "ASTRASYNC_WORKER_MAX_IN_FLIGHT_BATCHES";

    @Override
    public BatchTaskFactory create(Map<String, String> environment) {
        Objects.requireNonNull(environment, "environment must not be null");
        Path jobSpecPath = Path.of(required(environment, JOB_SPEC_ENVIRONMENT))
                .toAbsolutePath()
                .normalize();
        JobSpec jobSpec = readJobSpec(jobSpecPath);
        JdbcConnectorFactory connectorFactory = new JdbcConnectorFactory();
        CompiledJobPlan plan = new JobCompiler(ConnectorRegistry.of(connectorFactory)).compileCheckpointed(jobSpec);
        requireJdbcPlan(plan);

        JdbcRangeSplitSource splitSource =
                new JdbcRangeSplitSource(ConnectorConfiguration.of(plan.source().options()));
        ConnectorConfiguration sinkConfiguration =
                ConnectorConfiguration.of(plan.sink().options());
        int maxInFlightBatches = positiveInteger(environment, MAX_IN_FLIGHT_BATCHES_ENVIRONMENT, 1);
        return new BatchTaskFactory() {
            @Override
            public BatchTask create(io.astrasync.connector.api.source.SourceSplit split) {
                return createTask(split, io.astrasync.connector.api.source.SplitPosition.unbounded());
            }

            @Override
            public BatchTask create(
                    io.astrasync.connector.api.source.SourceSplit split, CheckpointExecutionContext context) {
                Objects.requireNonNull(context, "context must not be null");
                if ((plan.deliveryGuarantee() != io.astrasync.engine.jobspec.DeliveryGuarantee.AT_LEAST_ONCE
                                && plan.deliveryGuarantee()
                                        != io.astrasync.engine.jobspec.DeliveryGuarantee.EXACTLY_ONCE)
                        || !plan.source().options().containsKey("resumeColumn")) {
                    throw new IllegalArgumentException("checkpoint recovery requires an explicit JDBC resumeColumn");
                }
                return createTask(split, context.sourcePosition());
            }

            private BatchTask createTask(
                    io.astrasync.connector.api.source.SourceSplit split,
                    io.astrasync.connector.api.source.SplitPosition resumePosition) {
                BatchSource source = splitSource.createSource(split, resumePosition);
                try {
                    BatchSink sink = Objects.requireNonNull(
                            connectorFactory.createSink(sinkConfiguration), "JDBC connector returned a null Sink");
                    return new BatchTask(
                            split,
                            source,
                            sink,
                            plan.maxBatchRecords(),
                            maxInFlightBatches,
                            plan.deliveryGuarantee() == io.astrasync.engine.jobspec.DeliveryGuarantee.EXACTLY_ONCE);
                } catch (RuntimeException exception) {
                    try {
                        source.close();
                    } catch (RuntimeException closeFailure) {
                        exception.addSuppressed(closeFailure);
                    }
                    throw exception;
                }
            }
        };
    }

    private static JobSpec readJobSpec(Path path) {
        try {
            return new JobSpecParser().parse(Files.readString(path, StandardCharsets.UTF_8));
        } catch (IOException exception) {
            throw new IllegalArgumentException("failed to read Worker JobSpec", exception);
        }
    }

    private static void requireJdbcPlan(CompiledJobPlan plan) {
        if (!JdbcConnectorFactory.CONNECTOR_NAME.equals(plan.source().connector())
                || !JdbcConnectorFactory.CONNECTOR_NAME.equals(plan.sink().connector())) {
            throw new IllegalArgumentException("JDBC Worker provider requires JDBC source and sink connectors");
        }
    }

    private static String required(Map<String, String> environment, String name) {
        String value = environment.get(name);
        if (value == null || value.isBlank()) {
            throw new IllegalArgumentException("missing required environment variable " + name);
        }
        return value;
    }

    private static int positiveInteger(Map<String, String> environment, String name, int defaultValue) {
        String value = environment.get(name);
        if (value == null) {
            return defaultValue;
        }
        int parsed;
        try {
            parsed = Integer.parseInt(value);
        } catch (NumberFormatException exception) {
            throw new IllegalArgumentException("environment variable " + name + " must be an integer", exception);
        }
        if (parsed <= 0) {
            throw new IllegalArgumentException("environment variable " + name + " must be positive");
        }
        return parsed;
    }
}
