package io.astrasync.connector.mysql.cdc;

import static org.assertj.core.api.Assertions.assertThat;
import static org.junit.jupiter.api.Assumptions.assumeTrue;

import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.data.CdcBatch;
import io.astrasync.connector.api.data.DataEvent;
import io.astrasync.connector.api.source.CdcSource;
import io.astrasync.connector.api.source.SplitPosition;
import java.nio.file.Path;
import java.sql.Connection;
import java.sql.DriverManager;
import java.sql.SQLException;
import java.sql.Statement;
import java.time.Duration;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;
import org.testcontainers.DockerClientFactory;
import org.testcontainers.containers.MySQLContainer;

class MySqlCdcIntegrationTest {
    @TempDir
    Path temporaryDirectory;

    @Test
    void readsSnapshotThenAcknowledgedBinlogChanges() throws Exception {
        assumeTrue(dockerAvailable(), "Docker daemon is unavailable");

        try (MySQLContainer<?> database = new MySQLContainer<>("mysql:8.0.36")) {
            database.withDatabaseName("shop")
                    .withUsername("astrasync")
                    .withPassword("secret")
                    .withCommand(
                            "--server-id=1", "--log-bin=mysql-bin", "--binlog-format=ROW", "--binlog-row-image=FULL")
                    .start();

            grantCdcPrivileges(database);
            execute(
                    database.getJdbcUrl(),
                    database.getUsername(),
                    database.getPassword(),
                    "CREATE TABLE orders (" + "id BIGINT PRIMARY KEY, status VARCHAR(32) NOT NULL)");
            execute(
                    database.getJdbcUrl(),
                    database.getUsername(),
                    database.getPassword(),
                    "INSERT INTO orders (id, status) VALUES (1, 'NEW')");

            MySqlCdcConnectorFactory factory = new MySqlCdcConnectorFactory();
            CdcSource source = factory.createCdcSource(configuration(database));
            try (source) {
                source.openAt(SplitPosition.unbounded());
                CdcBatch snapshot = pollUntilSnapshot(source);
                SplitPosition snapshotPosition = source.acknowledge(snapshot);
                assertThat(snapshotPosition.isUnbounded()).isFalse();

                execute(
                        database.getJdbcUrl(),
                        database.getUsername(),
                        database.getPassword(),
                        "INSERT INTO orders (id, status) VALUES (2, 'PENDING')");
                execute(
                        database.getJdbcUrl(),
                        database.getUsername(),
                        database.getPassword(),
                        "UPDATE orders SET status = 'PAID' WHERE id = 1");
                execute(
                        database.getJdbcUrl(),
                        database.getUsername(),
                        database.getPassword(),
                        "DELETE FROM orders WHERE id = 2");

                List<DataEvent.Operation> operations = pollUntilOperations(source);
                assertThat(operations)
                        .contains(DataEvent.Operation.INSERT, DataEvent.Operation.UPDATE, DataEvent.Operation.DELETE);
            }
        }
    }

    private CdcBatch pollUntilSnapshot(CdcSource source) {
        long deadline = System.nanoTime() + Duration.ofSeconds(45).toNanos();
        while (System.nanoTime() < deadline) {
            CdcBatch batch = source.poll(Duration.ofMillis(500)).orElse(null);
            if (batch != null
                    && batch.snapshotCompleted()
                    && batch.events().stream()
                            .anyMatch(event -> event.getOperation() == DataEvent.Operation.SNAPSHOT)) {
                return batch;
            }
            if (batch != null) {
                source.acknowledge(batch);
            }
        }
        throw new AssertionError("timed out waiting for MySQL CDC snapshot");
    }

    private List<DataEvent.Operation> pollUntilOperations(CdcSource source) {
        long deadline = System.nanoTime() + Duration.ofSeconds(45).toNanos();
        List<DataEvent.Operation> operations = new ArrayList<>();
        while (System.nanoTime() < deadline) {
            CdcBatch batch = source.poll(Duration.ofMillis(500)).orElse(null);
            if (batch == null) {
                continue;
            }
            operations.addAll(
                    batch.events().stream().map(DataEvent::getOperation).toList());
            source.acknowledge(batch);
            if (operations.containsAll(
                    List.of(DataEvent.Operation.INSERT, DataEvent.Operation.UPDATE, DataEvent.Operation.DELETE))) {
                return operations;
            }
        }
        throw new AssertionError("timed out waiting for MySQL CDC changes: " + operations);
    }

    private static void grantCdcPrivileges(MySQLContainer<?> database) throws Exception {
        var result = database.execInContainer(
                "mysql",
                "--user=root",
                "--password=" + database.getPassword(),
                "--execute=GRANT SELECT, RELOAD, SHOW DATABASES, REPLICATION SLAVE, REPLICATION CLIENT "
                        + "ON *.* TO '"
                        + database.getUsername()
                        + "'@'%'");
        assertThat(result.getExitCode())
                .as("failed to grant MySQL CDC privileges:%n%s%n%s", result.getStdout(), result.getStderr())
                .isZero();
    }

    private ConnectorConfiguration configuration(MySQLContainer<?> database) {
        Map<String, String> values = new HashMap<>(Map.ofEntries(
                Map.entry("hostname", database.getHost()),
                Map.entry("port", Integer.toString(database.getMappedPort(MySQLContainer.MYSQL_PORT))),
                Map.entry("username", database.getUsername()),
                Map.entry("password", database.getPassword()),
                Map.entry("database", database.getDatabaseName()),
                Map.entry("tables", "shop.orders"),
                Map.entry("topicPrefix", "integration-mysql"),
                Map.entry("serverId", "5401"),
                Map.entry(
                        "schemaHistoryFile",
                        temporaryDirectory.resolve("mysql-history.dat").toString()),
                Map.entry("maxBatchSize", "1"),
                Map.entry("maxQueueSize", "16"),
                Map.entry("pollIntervalMillis", "100"),
                Map.entry("heartbeatIntervalMillis", "0"),
                Map.entry("queuedBatches", "8"),
                Map.entry("offsetCommitTimeoutMillis", "15000")));
        return ConnectorConfiguration.of(values);
    }

    private static void execute(String url, String username, String password, String sql) throws SQLException {
        try (Connection connection = DriverManager.getConnection(url, username, password);
                Statement statement = connection.createStatement()) {
            statement.execute(sql);
        }
    }

    private static boolean dockerAvailable() {
        try {
            return DockerClientFactory.instance().isDockerAvailable();
        } catch (RuntimeException exception) {
            return false;
        }
    }
}
