package io.astrasync.connector.jdbc;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.CheckpointContext;
import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.RecordKey;
import io.astrasync.connector.api.SinkCommitContext;
import io.astrasync.connector.api.SourcePosition;
import io.astrasync.connector.api.TraceContext;
import io.astrasync.connector.api.data.CdcBatch;
import io.astrasync.connector.api.data.CdcPhase;
import io.astrasync.connector.api.data.DataEvent;
import io.astrasync.connector.api.data.ImmutableDataEvent;
import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.sink.CdcSink;
import java.sql.Connection;
import java.sql.DriverManager;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.util.List;
import java.util.Map;
import java.util.UUID;
import org.junit.jupiter.api.BeforeEach;
import org.junit.jupiter.api.Test;

class JdbcCdcSinkTest {
    private String url;

    @BeforeEach
    void createDatabase() throws SQLException {
        url = "jdbc:h2:mem:cdc-" + UUID.randomUUID() + ";DB_CLOSE_DELAY=-1";
        try (Connection connection = DriverManager.getConnection(url);
                var statement = connection.createStatement()) {
            statement.execute(
                    "CREATE TABLE \"target_rows\" (\"id\" BIGINT PRIMARY KEY, \"status\" VARCHAR(32) NOT NULL)");
        }
    }

    @Test
    void transactionallyAppliesSnapshotUpdateAndDeleteEvents() throws SQLException {
        try (CdcSink sink = sink()) {
            sink.open(new CheckpointContext("job", 1, "cdc-0"));
            write(
                    sink,
                    1,
                    "digest-1",
                    "token-1",
                    batch(1, event(1, DataEvent.Operation.SNAPSHOT, null, row(1, "NEW"))));
            write(
                    sink,
                    2,
                    "digest-2",
                    "token-2",
                    batch(2, event(2, DataEvent.Operation.UPDATE, row(1, "NEW"), row(1, "PAID"))));

            assertThat(status(1)).isEqualTo("PAID");

            write(sink, 3, "digest-3", "token-3", batch(3, event(3, DataEvent.Operation.DELETE, row(1, "PAID"), null)));
            assertThat(status(1)).isNull();
            assertThat(sink.lastCommitToken()).isEqualTo("token-3");
        }
    }

    @Test
    void repeatedCommitTokenIsANoOpAndDigestMismatchIsRejected() throws SQLException {
        CdcBatch batch = batch(1, event(1, DataEvent.Operation.INSERT, null, row(1, "NEW")));
        try (CdcSink sink = sink()) {
            sink.open(new CheckpointContext("job", 1, "cdc-0"));
            SinkCommitContext context = context(1, "digest", "token");
            sink.writeBatch(batch, context);
            sink.writeBatch(batch, context);

            assertThat(rowCount()).isEqualTo(1);
            assertThatThrownBy(() -> sink.writeBatch(batch, context(1, "other-digest", "token")))
                    .isInstanceOf(IllegalStateException.class)
                    .hasMessageContaining("different CDC digest");
        }
    }

    @Test
    void rollsBackDataAndMarkerWhenAnEventCannotBeApplied() throws SQLException {
        CdcBatch batch = new CdcBatch(
                1,
                List.of(
                        event(1, DataEvent.Operation.INSERT, null, row(2, "NEW")),
                        event(2, DataEvent.Operation.TRUNCATE, null, null)),
                CdcPhase.STREAMING,
                false);
        try (CdcSink sink = sink()) {
            sink.open(new CheckpointContext("job", 1, "cdc-0"));

            assertThatThrownBy(() -> sink.writeBatch(batch, context(1, "digest", "token")))
                    .isInstanceOf(IllegalStateException.class)
                    .hasMessageContaining("truncate event is disabled");
            assertThat(rowCount()).isZero();
            assertThat(commitCount()).isZero();
        }
    }

    private CdcSink sink() {
        return new JdbcConnectorFactory()
                .createCdcSink(ConnectorConfiguration.of(Map.of(
                        "url", url,
                        "table", "target_rows",
                        "keyColumns", "id",
                        "commitTokenTable", "cdc_commit_tokens")));
    }

    private static void write(CdcSink sink, long sequence, String digest, String token, CdcBatch batch) {
        sink.writeBatch(batch, context(sequence, digest, token));
    }

    private static SinkCommitContext context(long sequence, String digest, String token) {
        return new SinkCommitContext("job", "cdc-0", sequence, digest, token);
    }

    private static CdcBatch batch(long sequence, DataEvent event) {
        return new CdcBatch(sequence, List.of(event), CdcPhase.STREAMING, false);
    }

    private static DataEvent event(long index, DataEvent.Operation operation, Row before, Row after) {
        Row beforeImage = before == null ? Row.empty() : before;
        Row afterImage = after == null ? Row.empty() : after;
        RecordKey key = operation == DataEvent.Operation.TRUNCATE
                ? RecordKey.empty()
                : RecordKey.of(Map.of("id", before == null ? after.get("id") : before.get("id")));
        return new ImmutableDataEvent(
                "event-" + index,
                SourcePosition.of(
                        "position-" + index,
                        "source",
                        "db",
                        "db.source_rows",
                        Map.of("pos", Long.toString(index)),
                        index,
                        "tx-" + index,
                        index),
                operation,
                index,
                index,
                "schema",
                "db.source_rows",
                key,
                beforeImage,
                afterImage,
                Map.of(),
                TraceContext.root());
    }

    private static Row row(long id, String status) {
        return Row.of(Map.of("id", id, "status", status));
    }

    private String status(long id) throws SQLException {
        try (Connection connection = DriverManager.getConnection(url);
                var statement =
                        connection.prepareStatement("SELECT \"status\" FROM \"target_rows\" WHERE \"id\" = ?")) {
            statement.setLong(1, id);
            try (ResultSet result = statement.executeQuery()) {
                return result.next() ? result.getString(1) : null;
            }
        }
    }

    private int rowCount() throws SQLException {
        return count("target_rows");
    }

    private int commitCount() throws SQLException {
        return count("cdc_commit_tokens");
    }

    private int count(String table) throws SQLException {
        try (Connection connection = DriverManager.getConnection(url);
                var statement = connection.createStatement();
                ResultSet result = statement.executeQuery("SELECT COUNT(*) FROM \"" + table + "\"")) {
            result.next();
            return result.getInt(1);
        }
    }
}
