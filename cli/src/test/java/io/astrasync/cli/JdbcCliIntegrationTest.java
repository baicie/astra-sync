package io.astrasync.cli;

import static org.assertj.core.api.Assertions.assertThat;

import io.astrasync.connector.file.CsvConnectorFactory;
import io.astrasync.connector.jdbc.JdbcConnectorFactory;
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
