package io.astrasync.cli;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.file.CsvConnectorFactory;
import io.astrasync.connector.jdbc.JdbcConnectorFactory;
import io.astrasync.engine.jobspec.JobSpec;
import io.astrasync.engine.jobspec.JobSpecParser;
import io.astrasync.engine.kernel.SyncJobException;
import io.astrasync.engine.kernel.SyncStage;
import io.astrasync.engine.local.LocalJobRunner;
import io.astrasync.engine.plan.ConnectorRegistry;
import java.io.IOException;
import java.io.PrintWriter;
import java.io.StringWriter;
import java.nio.charset.StandardCharsets;
import java.nio.file.Files;
import java.nio.file.Path;
import java.sql.Connection;
import java.sql.DriverManager;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.Statement;
import java.util.UUID;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;
import picocli.CommandLine;

class JdbcCliIntegrationTest {
    @TempDir
    Path tempDirectory;

    @Test
    void runsJdbcToFileThroughTheSameLocalRunner() throws Exception {
        String url = jdbcUrl();
        createTable(url, "CREATE TABLE \"source\" (\"id\" INT, \"name\" VARCHAR(100))");
        execute(url, "INSERT INTO \"source\" VALUES (1, 'Ada'), (2, 'Lin')");
        Path output = tempDirectory.resolve("jdbc-to-file.csv");
        Path job = write(
                "jdbc-to-file.yaml",
                jobYaml(
                        "jdbc",
                        "      url: '" + yaml(url)
                                + "'\n      query: 'SELECT \"id\" AS \"id\", \"name\" AS \"name\" FROM \"source\" ORDER BY \"id\"'",
                        "csv",
                        "      path: '" + yaml(output) + "'"));

        Invocation invocation = invoke(job);

        assertThat(invocation.exitCode()).isZero();
        assertThat(invocation.stderr()).isEmpty();
        assertThat(invocation.stdout()).contains("recordsRead=2", "recordsWritten=2");
        assertThat(Files.readString(output, StandardCharsets.UTF_8)).isEqualTo("id,name\r\n1,Ada\r\n2,Lin\r\n");
    }

    @Test
    void runsFileToJdbcAndCommitsBatches() throws Exception {
        String url = jdbcUrl();
        createTable(url, "CREATE TABLE \"target\" (\"id\" INT, \"name\" VARCHAR(100))");
        Path input = write("file-to-jdbc.csv", "id,name\r\n1,Ada\r\n2,Lin\r\n");
        Path job = write(
                "file-to-jdbc.yaml",
                jobYaml(
                        "csv",
                        "      path: '" + yaml(input) + "'",
                        "jdbc",
                        "      url: '" + yaml(url) + "'\n      table: target"));

        Invocation invocation = invoke(job);

        assertThat(invocation.exitCode()).isZero();
        assertThat(invocation.stderr()).isEmpty();
        try (Connection connection = DriverManager.getConnection(url);
                ResultSet result = connection
                        .createStatement()
                        .executeQuery("SELECT \"id\", \"name\" FROM \"target\" ORDER BY \"id\"")) {
            assertThat(result.next()).isTrue();
            assertThat(result.getInt(1)).isEqualTo(1);
            assertThat(result.getString(2)).isEqualTo("Ada");
            assertThat(result.next()).isTrue();
            assertThat(result.getInt(1)).isEqualTo(2);
            assertThat(result.getString(2)).isEqualTo("Lin");
            assertThat(result.next()).isFalse();
        }
    }

    @Test
    void csvCancellationLeavesOnlyTheCompletedBatchAndPartialMetrics() throws Exception {
        Path input = write("cancel-file-input.csv", "id,name\r\n1,Ada\r\n2,Lin\r\n");
        Path output = tempDirectory.resolve("cancel-file-output.csv");
        Path job = write(
                "cancel-file.yaml",
                jobYaml("csv", "      path: '" + yaml(input) + "'", "csv", "      path: '" + yaml(output) + "'"));
        LocalJobRunner runner = localRunner();

        assertThatThrownBy(() -> runner.run(parse(job), () -> fileHasContent(output)))
                .isInstanceOfSatisfying(SyncJobException.class, exception -> {
                    assertThat(exception.stage()).isEqualTo(SyncStage.CANCELLED);
                    assertThat(exception.partialResult().readCount()).isEqualTo(1);
                    assertThat(exception.partialResult().writtenCount()).isEqualTo(1);
                });
        assertThat(Files.readString(output, StandardCharsets.UTF_8)).isEqualTo("id,name\r\n1,Ada\r\n");
    }

    @Test
    void jdbcCancellationKeepsThePreviouslyCommittedBatch() throws Exception {
        String url = jdbcUrl();
        createTable(url, "CREATE TABLE \"target\" (\"id\" INT, \"name\" VARCHAR(100))");
        Path input = write("cancel-jdbc-input.csv", "id,name\r\n1,Ada\r\n2,Lin\r\n");
        Path job = write(
                "cancel-jdbc.yaml",
                jobYaml(
                        "csv",
                        "      path: '" + yaml(input) + "'",
                        "jdbc",
                        "      url: '" + yaml(url) + "'\n      table: target"));
        LocalJobRunner runner = localRunner();

        assertThatThrownBy(() -> runner.run(parse(job), () -> tableHasRows(url)))
                .isInstanceOfSatisfying(SyncJobException.class, exception -> {
                    assertThat(exception.stage()).isEqualTo(SyncStage.CANCELLED);
                    assertThat(exception.partialResult().readCount()).isEqualTo(1);
                    assertThat(exception.partialResult().writtenCount()).isEqualTo(1);
                });
        assertThat(rowCount(url)).isEqualTo(1);
    }

    private Invocation invoke(Path job) {
        StringWriter stdout = new StringWriter();
        StringWriter stderr = new StringWriter();
        CommandLine commandLine = AstraSyncCli.newCommandLine(
                () -> new LocalJobRunner(ConnectorRegistry.of(new CsvConnectorFactory(), new JdbcConnectorFactory())),
                new PrintWriter(stdout, true),
                new PrintWriter(stderr, true));
        int exitCode = commandLine.execute("run", job.toString());
        return new Invocation(exitCode, stdout.toString(), stderr.toString());
    }

    private static LocalJobRunner localRunner() {
        return new LocalJobRunner(ConnectorRegistry.of(new CsvConnectorFactory(), new JdbcConnectorFactory()));
    }

    private static JobSpec parse(Path job) throws IOException {
        return new JobSpecParser().parse(Files.readString(job, StandardCharsets.UTF_8));
    }

    private static boolean fileHasContent(Path path) {
        try {
            return Files.exists(path) && Files.size(path) > 0;
        } catch (IOException exception) {
            throw new IllegalStateException("failed to inspect cancellation output", exception);
        }
    }

    private static boolean tableHasRows(String url) {
        try {
            return rowCount(url) > 0;
        } catch (SQLException exception) {
            throw new IllegalStateException("failed to inspect cancellation target", exception);
        }
    }

    private static int rowCount(String url) throws SQLException {
        try (Connection connection = DriverManager.getConnection(url);
                ResultSet result = connection.createStatement().executeQuery("SELECT COUNT(*) FROM \"target\"")) {
            assertThat(result.next()).isTrue();
            return result.getInt(1);
        }
    }

    private Path write(String name, String content) throws IOException {
        Path path = tempDirectory.resolve(name);
        Files.writeString(path, content, StandardCharsets.UTF_8);
        return path;
    }

    private static String jobYaml(
            String sourceConnector, String sourceOptions, String sinkConnector, String sinkOptions) {
        return """
                apiVersion: sync.astrasync.io/v1
                kind: SyncJob
                metadata:
                  name: jdbc-cli
                spec:
                  source:
                    connector: %s
                    options:
                %s
                  sink:
                    connector: %s
                    options:
                %s
                  delivery:
                    guarantee: at-most-once
                  runtime:
                    maxBatchRecords: 1
                """
                .formatted(sourceConnector, sourceOptions, sinkConnector, sinkOptions);
    }

    private static void createTable(String url, String sql) throws SQLException {
        try (Connection connection = DriverManager.getConnection(url);
                Statement statement = connection.createStatement()) {
            statement.execute(sql);
        }
    }

    private static void execute(String url, String sql) throws SQLException {
        try (Connection connection = DriverManager.getConnection(url);
                Statement statement = connection.createStatement()) {
            statement.execute(sql);
        }
    }

    private static String jdbcUrl() {
        return "jdbc:h2:mem:cli_" + UUID.randomUUID().toString().replace('-', '_')
                + ";MODE=PostgreSQL;DB_CLOSE_DELAY=-1";
    }

    private static String yaml(Object value) {
        return value.toString().replace("'", "''");
    }

    private record Invocation(int exitCode, String stdout, String stderr) {}
}
