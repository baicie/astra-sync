package io.astrasync.engine.worker;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.jdbc.JdbcRangeSplitSource;
import io.astrasync.engine.runtime.BatchTask;
import io.astrasync.engine.runtime.BatchTaskFactory;
import io.astrasync.engine.runtime.WorkerResult;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.sql.Connection;
import java.sql.DriverManager;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.Statement;
import java.util.HashMap;
import java.util.Map;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class JdbcWorkerTaskFactoryProviderTest {
    @TempDir
    Path tempDirectory;

    @Test
    void loadsJobSpecAndMaterializesExecutableJdbcTasks() throws Exception {
        String url = jdbcUrl();
        execute(url, "CREATE TABLE SOURCE_DATA (ID INT NOT NULL, NAME VARCHAR(40))");
        execute(url, "CREATE TABLE TARGET_DATA (ID INT NOT NULL, NAME VARCHAR(40))");
        execute(url, "INSERT INTO SOURCE_DATA VALUES (1, 'Ada'), (2, 'Lin')");
        Path jobSpec = writeJob("jdbc", "jdbc", url);
        Map<String, String> environment = new HashMap<>(Map.of(
                JdbcWorkerTaskFactoryProvider.JOB_SPEC_ENVIRONMENT,
                jobSpec.toString(),
                JdbcWorkerTaskFactoryProvider.MAX_IN_FLIGHT_BATCHES_ENVIRONMENT,
                "3"));
        BatchTaskFactory taskFactory = new JdbcWorkerTaskFactoryProvider().create(environment);
        SourceSplit split =
                new JdbcRangeSplitSource(splitConfiguration(url)).enumerate().get(0);

        BatchTask task = taskFactory.create(split);
        WorkerResult result = new InProcessBatchWorker("worker-0").execute(task);

        assertThat(task.maxBatchRecords()).isEqualTo(2);
        assertThat(task.maxInFlightBatches()).isEqualTo(3);
        assertThat(result.taskId()).isEqualTo(split.splitId());
        assertThat(result.metrics().readCount()).isEqualTo(2);
        assertThat(result.metrics().writtenCount()).isEqualTo(2);
        assertThat(readTarget(url)).containsExactly("1:Ada", "2:Lin");
    }

    @Test
    void rejectsMissingInvalidAndNonJdbcConfiguration() throws Exception {
        assertThatThrownBy(() -> new JdbcWorkerTaskFactoryProvider().create(Map.of()))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("missing required environment variable ASTRASYNC_WORKER_JOB_SPEC");

        assertThatThrownBy(() -> new JdbcWorkerTaskFactoryProvider()
                        .create(Map.of(JdbcWorkerTaskFactoryProvider.JOB_SPEC_ENVIRONMENT, "missing.yaml")))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("failed to read Worker JobSpec");

        String url = jdbcUrl();
        Path validJob = writeJob("jdbc", "jdbc", url);
        assertThatThrownBy(() -> new JdbcWorkerTaskFactoryProvider()
                        .create(Map.of(
                                JdbcWorkerTaskFactoryProvider.JOB_SPEC_ENVIRONMENT,
                                validJob.toString(),
                                JdbcWorkerTaskFactoryProvider.MAX_IN_FLIGHT_BATCHES_ENVIRONMENT,
                                "0")))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("environment variable ASTRASYNC_WORKER_MAX_IN_FLIGHT_BATCHES must be positive");

        Path nonJdbcJob = writeJob("csv", "jdbc", url);
        assertThatThrownBy(() -> new JdbcWorkerTaskFactoryProvider()
                        .create(Map.of(JdbcWorkerTaskFactoryProvider.JOB_SPEC_ENVIRONMENT, nonJdbcJob.toString())))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("SOURCE connector is not registered: csv");
    }

    private Path writeJob(String sourceConnector, String sinkConnector, String url) throws IOException {
        Path path = tempDirectory.resolve(sourceConnector + "-to-" + sinkConnector + ".yaml");
        String sourceOptions = sourceConnector.equals("jdbc")
                ? """
                      url: '%s'
                      table: SOURCE_DATA
                      splitColumn: ID
                      splitCount: '1'
                  """
                        .formatted(yaml(url))
                : """
                      path: input.csv
                  """;
        Files.writeString(
                path,
                """
                apiVersion: sync.astrasync.io/v1
                kind: SyncJob
                metadata:
                  name: jdbc-worker-test
                spec:
                  source:
                    connector: %s
                    options:
                %s
                  sink:
                    connector: %s
                    options:
                      url: '%s'
                      table: TARGET_DATA
                  delivery:
                    guarantee: at-most-once
                  runtime:
                    maxBatchRecords: 2
                """
                        .formatted(sourceConnector, indent(sourceOptions), sinkConnector, yaml(url)),
                StandardCharsets.UTF_8);
        return path;
    }

    private static ConnectorConfiguration splitConfiguration(String url) {
        return ConnectorConfiguration.of(Map.of(
                "url", url,
                "table", "SOURCE_DATA",
                "splitColumn", "ID",
                "splitCount", "1"));
    }

    private static java.util.List<String> readTarget(String url) throws SQLException {
        try (Connection connection = DriverManager.getConnection(url);
                ResultSet result =
                        connection.createStatement().executeQuery("SELECT ID, NAME FROM TARGET_DATA ORDER BY ID")) {
            java.util.List<String> rows = new java.util.ArrayList<>();
            while (result.next()) {
                rows.add(result.getInt(1) + ":" + result.getString(2));
            }
            return rows;
        }
    }

    private static void execute(String url, String sql) throws SQLException {
        try (Connection connection = DriverManager.getConnection(url);
                Statement statement = connection.createStatement()) {
            statement.execute(sql);
        }
    }

    private static String jdbcUrl() {
        return "jdbc:h2:mem:worker_" + UUID.randomUUID().toString().replace('-', '_')
                + ";MODE=PostgreSQL;DB_CLOSE_DELAY=-1";
    }

    private static String yaml(String value) {
        return value.replace("'", "''");
    }

    private static String indent(String value) {
        return value.indent(4).stripTrailing();
    }
}
