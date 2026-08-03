package io.astrasync.connector.jdbc;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.source.BatchSource;
import java.math.BigDecimal;
import java.nio.charset.StandardCharsets;
import java.sql.Connection;
import java.sql.SQLException;
import java.sql.Statement;
import java.time.LocalDate;
import java.time.LocalDateTime;
import java.util.Map;
import org.junit.jupiter.api.Test;

class JdbcBatchSourceTest {
    private final JdbcConnectorFactory factory = new JdbcConnectorFactory();

    @Test
    void readsTypedValuesInMetadataOrderAndRespectsBatchBounds() throws Exception {
        String url = JdbcTestSupport.url();
        createSourceTable(url);

        BatchSource source = factory.createSource(ConnectorConfiguration.of(Map.of(
                "url",
                url,
                "query",
                "SELECT \"id\" AS \"id\", \"name\" AS \"name\", \"amount\" AS \"amount\", "
                        + "\"event_date\" AS \"event_date\", \"event_time\" AS \"event_time\", "
                        + "\"payload\" AS \"payload\", \"note\" AS \"note\", \"flag\" AS \"flag\" "
                        + "FROM \"records\" ORDER BY \"id\"",
                "fetchSize",
                "1")));
        source.open();
        try {
            RowBatch first = source.readBatch(1);
            RowBatch second = source.readBatch(1);
            RowBatch last = source.readBatch(1);

            assertThat(first.endOfInput()).isFalse();
            assertThat(second.endOfInput()).isFalse();
            assertThat(last.endOfInput()).isTrue();
            assertThat(first.rows().get(0).asMap().keySet())
                    .containsExactly("id", "name", "amount", "event_date", "event_time", "payload", "note", "flag");
            Row firstRow = first.rows().get(0);
            assertThat(firstRow.get("id")).isEqualTo(1);
            assertThat(firstRow.get("name")).isEqualTo("Ada 你好");
            assertThat(firstRow.get("amount")).isEqualTo(new BigDecimal("12.3400"));
            assertThat(firstRow.get("event_date")).isEqualTo(LocalDate.of(2026, 8, 3));
            assertThat(firstRow.get("event_time")).isEqualTo(LocalDateTime.of(2026, 8, 3, 12, 34, 56, 123_000_000));
            assertThat(firstRow.get("payload")).isEqualTo("bytes".getBytes(StandardCharsets.UTF_8));
            assertThat(firstRow.get("note")).isEqualTo("large text");
            assertThat(firstRow.get("flag")).isEqualTo(true);
            assertThat(second.rows().get(0).get("name")).isNull();
            assertThat(second.rows().get(0).get("payload")).isNull();
        } finally {
            source.close();
        }
    }

    @Test
    void rejectsDuplicateLabelsBeforeReturningRows() throws Exception {
        String url = JdbcTestSupport.url();
        try (BatchSource source = factory.createSource(
                ConnectorConfiguration.of(Map.of("url", url, "query", "SELECT 1 AS same, 2 AS same")))) {
            assertThatThrownBy(source::open)
                    .isInstanceOf(IllegalStateException.class)
                    .hasMessageContaining("failed to open JDBC source")
                    .hasRootCauseMessage("JDBC query has duplicate column label 'SAME'");
        }
    }

    @Test
    void reportsUnsupportedTypesWithColumnEvidence() throws Exception {
        String url = JdbcTestSupport.url();
        try (BatchSource source = factory.createSource(
                ConnectorConfiguration.of(Map.of("url", url, "query", "SELECT ARRAY[1, 2] AS \"arr\"")))) {
            source.open();
            assertThatThrownBy(() -> source.readBatch(10))
                    .isInstanceOf(IllegalStateException.class)
                    .hasMessageContaining("record 1")
                    .hasMessageContaining("arr");
        }
    }

    @Test
    void rejectsInvalidBatchRequestsAndCallsAfterEnd() throws Exception {
        String url = JdbcTestSupport.url();
        createSourceTable(url);
        BatchSource source = factory.createSource(
                ConnectorConfiguration.of(Map.of("url", url, "query", "SELECT \"id\" FROM \"records\"")));
        assertThatThrownBy(() -> source.readBatch(1)).isInstanceOf(IllegalStateException.class);
        source.open();
        try {
            assertThatThrownBy(() -> source.readBatch(0)).isInstanceOf(IllegalArgumentException.class);
        } finally {
            source.close();
        }
    }

    private static void createSourceTable(String url) throws SQLException {
        try (Connection connection = JdbcTestSupport.connect(url);
                Statement statement = connection.createStatement()) {
            statement.execute("CREATE TABLE \"records\" ("
                    + "\"id\" INT, \"name\" VARCHAR(200), \"amount\" DECIMAL(20,4), \"event_date\" DATE, "
                    + "\"event_time\" TIMESTAMP, \"payload\" VARBINARY(32), \"note\" CLOB, \"flag\" BOOLEAN)");
            statement.execute("INSERT INTO \"records\" VALUES "
                    + "(1, 'Ada 你好', 12.3400, DATE '2026-08-03', TIMESTAMP '2026-08-03 12:34:56.123', "
                    + "X'6279746573', 'large text', TRUE), "
                    + "(2, NULL, NULL, NULL, NULL, NULL, NULL, FALSE)");
        }
    }
}
