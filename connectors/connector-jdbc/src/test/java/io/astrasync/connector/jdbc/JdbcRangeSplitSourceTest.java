package io.astrasync.connector.jdbc;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.connector.api.source.CheckpointableBatchSource;
import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitPosition;
import java.sql.Connection;
import java.sql.SQLException;
import java.sql.Statement;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;

class JdbcRangeSplitSourceTest {
    @Test
    void enumeratesRangesAndMaterializesBoundedReaders() throws Exception {
        String url = JdbcTestSupport.url();
        createRangeTable(url, "CREATE TABLE RECORDS (ID INT NOT NULL, PAYLOAD VARCHAR(20))", 1, 2, 3, 4, 5);
        JdbcRangeSplitSource source = new JdbcRangeSplitSource(configuration(url, "RECORDS", "ID", "2"));

        List<SourceSplit> splits = source.enumerate();

        assertThat(splits).hasSize(2);
        assertThat(splits.get(0).start().offsets()).containsEntry("ID", "1");
        assertThat(splits.get(0).end().offsets()).containsEntry("ID", "3");
        assertThat(splits.get(1).start().offsets()).containsEntry("ID", "3");
        assertThat(splits.get(1).end().isUnbounded()).isTrue();
        assertThat(readIds(source, splits.get(0))).containsExactly(1, 2);
        assertThat(readIds(source, splits.get(1))).containsExactly(3, 4, 5);
    }

    @Test
    void capsSplitCountAtTheNumberOfIntegerValues() throws Exception {
        String url = JdbcTestSupport.url();
        createRangeTable(url, "CREATE TABLE RECORDS (ID INT NOT NULL)", 7, 8, 9);

        List<SourceSplit> splits = new JdbcRangeSplitSource(configuration(url, "RECORDS", "ID", "10")).enumerate();

        assertThat(splits).hasSize(3);
        assertThat(splits.get(0).start().offsets()).containsEntry("ID", "7");
        assertThat(splits.get(0).end().offsets()).containsEntry("ID", "8");
        assertThat(splits.get(1).end().offsets()).containsEntry("ID", "9");
        assertThat(splits.get(2).end().isUnbounded()).isTrue();
    }

    @Test
    void returnsNoSplitsForAnEmptyTable() throws Exception {
        String url = JdbcTestSupport.url();
        createRangeTable(url, "CREATE TABLE RECORDS (ID INT NOT NULL)");

        assertThat(new JdbcRangeSplitSource(configuration(url, "RECORDS", "ID", "4")).enumerate())
                .isEmpty();
    }

    @Test
    void rejectsNonIntegralRangesAndInvalidConfiguration() throws Exception {
        String url = JdbcTestSupport.url();
        createRangeTable(url, "CREATE TABLE RECORDS (ID DECIMAL(10, 2) NOT NULL)", "1.50", "2.50");

        assertThatThrownBy(() -> new JdbcRangeSplitSource(configuration(url, "RECORDS", "ID", "2")).enumerate())
                .isInstanceOf(IllegalStateException.class)
                .hasMessage("JDBC split minimum is not an integer");
        assertThatThrownBy(() -> new JdbcRangeSplitSource(configuration(url, "RECORDS", "ID", "0")))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("splitCount");
        assertThatThrownBy(() -> new JdbcRangeSplitSource(ConnectorConfiguration.of(
                        Map.of("url", url, "table", "RECORDS", "splitColumn", "ID", "unknown", "value"))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("unknown JDBC split option");
    }

    @Test
    void rejectsInvalidMaterializedBoundaries() throws Exception {
        String url = JdbcTestSupport.url();
        createRangeTable(url, "CREATE TABLE RECORDS (ID INT NOT NULL)", 1, 2);
        JdbcRangeSplitSource source = new JdbcRangeSplitSource(configuration(url, "RECORDS", "ID", "2"));

        assertThatThrownBy(() -> source.createSource(new SourceSplit(
                        "wrong-source", "jdbc:OTHER", new SplitPosition(Map.of("ID", "1")), SplitPosition.unbounded())))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("belongs to source");
        assertThatThrownBy(() -> source.createSource(new SourceSplit(
                        "both-unbounded", "jdbc:RECORDS", SplitPosition.unbounded(), SplitPosition.unbounded())))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("JDBC split must have at least one bounded boundary");
        assertThatThrownBy(() -> source.createSource(new SourceSplit(
                        "reversed",
                        "jdbc:RECORDS",
                        new SplitPosition(Map.of("ID", "2")),
                        new SplitPosition(Map.of("ID", "2")))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("split start must be less than split end");
        assertThatThrownBy(() -> source.createSource(new SourceSplit(
                        "wrong-key",
                        "jdbc:RECORDS",
                        new SplitPosition(Map.of("OTHER", "1")),
                        SplitPosition.unbounded())))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("must contain only 'ID'");
        assertThatThrownBy(() -> source.createSource(new SourceSplit(
                        "not-integer",
                        "jdbc:RECORDS",
                        new SplitPosition(Map.of("ID", "1.5")),
                        SplitPosition.unbounded())))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("JDBC split boundary must be an integer");
    }

    @Test
    void resumesWithinTheOriginalRangeFromAnExplicitResumeColumn() throws Exception {
        String url = JdbcTestSupport.url();
        createRangeTable(url, "CREATE TABLE RECORDS (ID INT NOT NULL, PAYLOAD VARCHAR(20))", 1, 2, 3, 4, 5);
        JdbcRangeSplitSource source = new JdbcRangeSplitSource(ConnectorConfiguration.of(Map.of(
                "url", url,
                "table", "RECORDS",
                "splitColumn", "ID",
                "resumeColumn", "ID",
                "splitCount", "1")));
        SourceSplit split = source.enumerate().getFirst();

        CheckpointableBatchSource reader =
                (CheckpointableBatchSource) source.createSource(split, new SplitPosition(Map.of("ID", "2")));
        List<Integer> ids = new ArrayList<>();
        try (reader) {
            reader.openAt(new SplitPosition(Map.of("ID", "2")));
            while (true) {
                RowBatch batch = reader.readBatch(2);
                ids.addAll(batch.rows().stream()
                        .map(row -> (Integer) row.get("ID"))
                        .toList());
                if (batch.endOfInput()) {
                    break;
                }
            }
        }
        assertThat(ids).containsExactly(3, 4, 5);
    }

    @Test
    void rejectsNullableOrDuplicateResumeValuesBeforeCreatingSplits() throws Exception {
        String duplicateUrl = JdbcTestSupport.url();
        try (Connection connection = JdbcTestSupport.connect(duplicateUrl);
                Statement statement = connection.createStatement()) {
            statement.execute("CREATE TABLE RECORDS (ID INT NOT NULL, RESUME_KEY INT)");
            statement.execute("INSERT INTO RECORDS VALUES (1, 7), (2, 7)");
        }
        assertThatThrownBy(() -> new JdbcRangeSplitSource(ConnectorConfiguration.of(Map.of(
                                "url", duplicateUrl,
                                "table", "RECORDS",
                                "splitColumn", "ID",
                                "resumeColumn", "RESUME_KEY")))
                        .enumerate())
                .isInstanceOf(IllegalStateException.class)
                .hasMessage("JDBC resumeColumn must be unique");

        String nullableUrl = JdbcTestSupport.url();
        try (Connection connection = JdbcTestSupport.connect(nullableUrl);
                Statement statement = connection.createStatement()) {
            statement.execute("CREATE TABLE RECORDS (ID INT NOT NULL, RESUME_KEY INT)");
            statement.execute("INSERT INTO RECORDS VALUES (1, NULL), (2, 8)");
        }
        assertThatThrownBy(() -> new JdbcRangeSplitSource(ConnectorConfiguration.of(Map.of(
                                "url", nullableUrl,
                                "table", "RECORDS",
                                "splitColumn", "ID",
                                "resumeColumn", "RESUME_KEY")))
                        .enumerate())
                .isInstanceOf(IllegalStateException.class)
                .hasMessage("JDBC resumeColumn must be non-null");
    }

    private static ConnectorConfiguration configuration(
            String url, String table, String splitColumn, String splitCount) {
        return ConnectorConfiguration.of(Map.of(
                "url", url,
                "table", table,
                "splitColumn", splitColumn,
                "splitCount", splitCount));
    }

    private static List<Integer> readIds(JdbcRangeSplitSource source, SourceSplit split) {
        List<Integer> ids = new ArrayList<>();
        try (BatchSource reader = source.createSource(split)) {
            reader.open();
            while (true) {
                RowBatch batch = reader.readBatch(2);
                ids.addAll(batch.rows().stream()
                        .map(row -> (Integer) row.get("ID"))
                        .toList());
                if (batch.endOfInput()) {
                    return ids;
                }
            }
        }
    }

    private static void createRangeTable(String url, String createSql, Object... values) throws SQLException {
        try (Connection connection = JdbcTestSupport.connect(url);
                Statement statement = connection.createStatement()) {
            statement.execute(createSql);
            for (Object value : values) {
                statement.execute(
                        "INSERT INTO RECORDS VALUES (" + value + (createSql.contains("PAYLOAD") ? ", 'row')" : ")"));
            }
        }
    }
}
