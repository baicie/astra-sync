package io.astrasync.connector.jdbc;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.CheckpointContext;
import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.SinkCommitContext;
import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.sink.IdempotentBatchSink;
import java.math.BigDecimal;
import java.nio.charset.StandardCharsets;
import java.sql.Connection;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.Statement;
import java.time.LocalDate;
import java.time.LocalDateTime;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

class JdbcBatchSinkTest {
    private final JdbcConnectorFactory factory = new JdbcConnectorFactory();

    @Test
    void commitsEachBatchAndPreservesTypedValues() throws Exception {
        String url = JdbcTestSupport.url();
        createTargetTable(url, false);
        BatchSink sink = sink(url);
        sink.open();
        try {
            sink.writeBatch(RowBatch.data(List.of(Row.of(Map.of(
                    "id",
                    1,
                    "name",
                    "Ada 你好",
                    "amount",
                    new BigDecimal("12.3400"),
                    "event_date",
                    LocalDate.of(2026, 8, 3),
                    "event_time",
                    LocalDateTime.of(2026, 8, 3, 12, 34, 56),
                    "payload",
                    "bytes".getBytes(StandardCharsets.UTF_8),
                    "note",
                    "large text",
                    "flag",
                    true)))));
            sink.writeBatch(RowBatch.data(List.of(Row.of(Map.of(
                    "id",
                    2,
                    "name",
                    "second",
                    "amount",
                    new BigDecimal("1.00"),
                    "event_date",
                    LocalDate.of(2026, 8, 4),
                    "event_time",
                    LocalDateTime.of(2026, 8, 4, 1, 2),
                    "payload",
                    new byte[] {1, 2},
                    "note",
                    "more",
                    "flag",
                    false)))));
        } finally {
            sink.close();
        }

        try (Connection connection = JdbcTestSupport.connect(url);
                ResultSet result =
                        connection.createStatement().executeQuery("SELECT * FROM \"target\" ORDER BY \"id\"")) {
            assertThat(result.next()).isTrue();
            assertThat(result.getInt("id")).isEqualTo(1);
            assertThat(result.getString("name")).isEqualTo("Ada 你好");
            assertThat(result.getBigDecimal("amount")).isEqualByComparingTo("12.3400");
            assertThat(result.getBytes("payload")).isEqualTo("bytes".getBytes(StandardCharsets.UTF_8));
            assertThat(result.getString("note")).isEqualTo("large text");
            assertThat(result.next()).isTrue();
            assertThat(result.getInt("id")).isEqualTo(2);
            assertThat(result.next()).isFalse();
        }
    }

    @Test
    void rollsBackARejectedBatchWithoutCommittingClose() throws Exception {
        String url = JdbcTestSupport.url();
        createTargetTable(url, true);
        BatchSink sink = sink(url);
        sink.open();
        try {
            sink.writeBatch(RowBatch.data(List.of(row(1))));
            assertThatThrownBy(() -> sink.writeBatch(RowBatch.data(List.of(row(1), row(2)))))
                    .isInstanceOf(IllegalStateException.class)
                    .hasMessageContaining("failed to write JDBC sink");
        } finally {
            sink.close();
        }

        try (Connection connection = JdbcTestSupport.connect(url);
                ResultSet result =
                        connection.createStatement().executeQuery("SELECT \"id\" FROM \"target\" ORDER BY \"id\"")) {
            assertThat(result.next()).isTrue();
            assertThat(result.getInt(1)).isEqualTo(1);
            assertThat(result.next()).isFalse();
        }
    }

    @Test
    void rejectsSchemaDriftAndUnsupportedValuesBeforeExecution() throws Exception {
        String url = JdbcTestSupport.url();
        createTargetTable(url, false);
        BatchSink sink = sink(url);
        sink.open();
        try {
            Row first = Row.of(Map.of(
                    "id",
                    1,
                    "name",
                    "first",
                    "amount",
                    BigDecimal.ONE,
                    "event_date",
                    LocalDate.of(2026, 1, 1),
                    "event_time",
                    LocalDateTime.of(2026, 1, 1, 0, 0),
                    "payload",
                    new byte[] {1},
                    "note",
                    "note",
                    "flag",
                    true));
            sink.writeBatch(RowBatch.data(List.of(first)));
            assertThatThrownBy(() -> sink.writeBatch(RowBatch.data(List.of(Row.of("other", "value")))))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("must match");
            assertThatThrownBy(() -> sink.writeBatch(RowBatch.data(List.of(first.with("name", new Object())))))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("name");
        } finally {
            sink.close();
        }
    }

    @Test
    void emptyBatchesDoNotInferSchemaOrWriteRows() throws Exception {
        String url = JdbcTestSupport.url();
        createTargetTable(url, false);
        BatchSink sink = sink(url);
        sink.open();
        try {
            sink.writeBatch(RowBatch.end());
        } finally {
            sink.close();
        }
        try (Connection connection = JdbcTestSupport.connect(url);
                ResultSet result = connection.createStatement().executeQuery("SELECT COUNT(*) FROM \"target\"")) {
            assertThat(result.next()).isTrue();
            assertThat(result.getInt(1)).isZero();
        }
    }

    @Test
    void retriesACommittedTokenWithoutDuplicatingRows() throws Exception {
        String url = JdbcTestSupport.url();
        createTargetTable(url, false);
        SinkCommitContext commit = new SinkCommitContext("orders", "split-0", 1, "digest-1", "token-1");

        IdempotentBatchSink first = idempotentSink(url);
        first.open(new CheckpointContext("orders", 1, "split-0"));
        first.writeBatch(RowBatch.data(List.of(row(1))), commit);
        first.close();

        IdempotentBatchSink retry = idempotentSink(url);
        retry.open(new CheckpointContext("orders", 2, "split-0"));
        retry.writeBatch(RowBatch.data(List.of(row(1))), commit);
        assertThat(retry.lastCommitToken()).isEqualTo("token-1");
        retry.close();

        try (Connection connection = JdbcTestSupport.connect(url);
                ResultSet result = connection.createStatement().executeQuery("SELECT COUNT(*) FROM \"target\"")) {
            assertThat(result.next()).isTrue();
            assertThat(result.getInt(1)).isEqualTo(1);
        }
    }

    @Test
    void rejectsACommitTokenWithADifferentBatchDigest() throws Exception {
        String url = JdbcTestSupport.url();
        createTargetTable(url, false);
        IdempotentBatchSink sink = idempotentSink(url);
        sink.open(new CheckpointContext("orders", 1, "split-0"));
        sink.writeBatch(
                RowBatch.data(List.of(row(1))), new SinkCommitContext("orders", "split-0", 1, "digest-1", "token-1"));
        sink.close();

        IdempotentBatchSink retry = idempotentSink(url);
        retry.open(new CheckpointContext("orders", 2, "split-0"));
        assertThatThrownBy(() -> retry.writeBatch(
                        RowBatch.data(List.of(row(1))),
                        new SinkCommitContext("orders", "split-0", 1, "digest-2", "token-1")))
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("different batch digest");
        retry.close();
    }

    private BatchSink sink(String url) {
        return factory.createSink(ConnectorConfiguration.of(Map.of("url", url, "table", "target")));
    }

    private IdempotentBatchSink idempotentSink(String url) {
        return (IdempotentBatchSink) sink(url);
    }

    private static Row row(int id) {
        return Row.of(Map.of(
                "id",
                id,
                "name",
                "row " + id,
                "amount",
                BigDecimal.ONE,
                "event_date",
                LocalDate.of(2026, 1, 1),
                "event_time",
                LocalDateTime.of(2026, 1, 1, 0, 0),
                "payload",
                new byte[] {1},
                "note",
                "note",
                "flag",
                true));
    }

    private static void createTargetTable(String url, boolean primaryKey) throws SQLException {
        try (Connection connection = JdbcTestSupport.connect(url);
                Statement statement = connection.createStatement()) {
            statement.execute("CREATE TABLE \"target\" ("
                    + "\"id\" INT " + (primaryKey ? "PRIMARY KEY" : "")
                    + ", \"name\" VARCHAR(200), \"amount\" DECIMAL(20,4), \"event_date\" DATE, \"event_time\" TIMESTAMP, "
                    + "\"payload\" VARBINARY(32), \"note\" CLOB, \"flag\" BOOLEAN)");
        }
    }
}
