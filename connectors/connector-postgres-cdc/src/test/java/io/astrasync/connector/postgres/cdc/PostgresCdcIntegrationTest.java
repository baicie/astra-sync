package io.astrasync.connector.postgres.cdc;

import static org.assertj.core.api.Assertions.assertThat;
import static org.junit.jupiter.api.Assumptions.assumeTrue;

import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.data.CdcBatch;
import io.astrasync.connector.api.data.DataEvent;
import io.astrasync.connector.api.source.CdcSource;
import io.astrasync.connector.api.source.SplitPosition;
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
import org.testcontainers.DockerClientFactory;
import org.testcontainers.containers.PostgreSQLContainer;

class PostgresCdcIntegrationTest {
    @Test
    void readsSnapshotThenAcknowledgedLogicalReplicationChanges() throws Exception {
        assumeTrue(dockerAvailable(), "Docker daemon is unavailable");

        try (PostgreSQLContainer<?> database = new PostgreSQLContainer<>("postgres:16-alpine")) {
            database.withDatabaseName("shop")
                    .withUsername("astrasync")
                    .withPassword("secret")
                    .withCommand(
                            "postgres",
                            "-c",
                            "wal_level=logical",
                            "-c",
                            "max_replication_slots=4",
                            "-c",
                            "max_wal_senders=4")
                    .start();

            execute(
                    database.getJdbcUrl(),
                    database.getUsername(),
                    database.getPassword(),
                    "CREATE TABLE public.orders (" + "id BIGINT PRIMARY KEY, status VARCHAR(32) NOT NULL)");
            execute(
                    database.getJdbcUrl(),
                    database.getUsername(),
                    database.getPassword(),
                    "INSERT INTO public.orders (id, status) VALUES (1, 'NEW')");

            PostgresCdcConnectorFactory factory = new PostgresCdcConnectorFactory();
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
                        "INSERT INTO public.orders (id, status) VALUES (2, 'PENDING')");
                execute(
                        database.getJdbcUrl(),
                        database.getUsername(),
                        database.getPassword(),
                        "UPDATE public.orders SET status = 'PAID' WHERE id = 1");
                execute(
                        database.getJdbcUrl(),
                        database.getUsername(),
                        database.getPassword(),
                        "DELETE FROM public.orders WHERE id = 2");

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
        throw new AssertionError("timed out waiting for PostgreSQL CDC snapshot");
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
        throw new AssertionError("timed out waiting for PostgreSQL CDC changes: " + operations);
    }

    private static ConnectorConfiguration configuration(PostgreSQLContainer<?> database) {
        Map<String, String> values = new HashMap<>(Map.ofEntries(
                Map.entry("hostname", database.getHost()),
                Map.entry("port", Integer.toString(database.getMappedPort(PostgreSQLContainer.POSTGRESQL_PORT))),
                Map.entry("username", database.getUsername()),
                Map.entry("password", database.getPassword()),
                Map.entry("database", database.getDatabaseName()),
                Map.entry("schemas", "public"),
                Map.entry("tables", "public.orders"),
                Map.entry("topicPrefix", "integration-postgres"),
                Map.entry("slotName", "astrasync_integration_slot"),
                Map.entry("publicationName", "astrasync_integration_publication"),
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
