package io.astrasync.connector.jdbc;

import io.astrasync.connector.api.CheckpointContext;
import io.astrasync.connector.api.RecordKey;
import io.astrasync.connector.api.SinkCommitContext;
import io.astrasync.connector.api.data.CdcBatch;
import io.astrasync.connector.api.data.DataEvent;
import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.sink.CdcSink;
import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.sql.Statement;
import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;

/** JDBC CDC sink applying source images as transactional upserts and deletes. */
final class JdbcCdcSink implements CdcSink {
    private final JdbcCdcSinkOptions options;
    private State state = State.NEW;
    private Connection connection;
    private CheckpointContext checkpointContext;
    private String lastCommitToken;
    private boolean commitTokenTableReady;

    JdbcCdcSink(JdbcCdcSinkOptions options) {
        this.options = options;
    }

    @Override
    public void open(CheckpointContext context) {
        if (state != State.NEW) {
            throw new IllegalStateException("cannot open JDBC CDC sink while state is " + state);
        }
        checkpointContext = Objects.requireNonNull(context, "context must not be null");
        try {
            connection = JdbcConnectorOptions.connect(options.url(), options.user(), options.password());
            connection.setAutoCommit(false);
            state = State.OPEN;
        } catch (SQLException | RuntimeException exception) {
            state = State.CLOSED;
            closeQuietly(connection, exception);
            throw new IllegalStateException("failed to open JDBC CDC sink", exception);
        }
    }

    @Override
    public void writeBatch(CdcBatch batch, SinkCommitContext commitContext) {
        requireOpen("write");
        Objects.requireNonNull(batch, "batch must not be null");
        validateCommitContext(commitContext);
        RuntimeException failure = null;
        try {
            ensureCommitTokenTable();
            String recordedDigest = findCommitDigest(commitContext.commitToken());
            if (recordedDigest != null) {
                if (!recordedDigest.equals(commitContext.batchDigest())) {
                    throw new IllegalStateException("commit token is already associated with a different CDC digest");
                }
                connection.commit();
                lastCommitToken = commitContext.commitToken();
                return;
            }
            for (DataEvent event : batch.events()) {
                apply(event);
            }
            insertCommitMarker(commitContext);
            checkpointContext.assertCurrent();
            connection.commit();
            lastCommitToken = commitContext.commitToken();
        } catch (SQLException | IllegalArgumentException exception) {
            failure = exception instanceof SQLException
                    ? new IllegalStateException("failed to write JDBC CDC batch", exception)
                    : (IllegalArgumentException) exception;
            rollback(failure);
            throw failure;
        } catch (RuntimeException exception) {
            failure = exception;
            rollback(failure);
            throw failure;
        }
    }

    @Override
    public String lastCommitToken() {
        requireOpen("read commit token");
        if (lastCommitToken == null) {
            throw new IllegalStateException("JDBC CDC sink has not committed a batch");
        }
        return lastCommitToken;
    }

    @Override
    public void close() {
        if (state == State.CLOSED) {
            return;
        }
        state = State.CLOSED;
        Connection openedConnection = connection;
        connection = null;
        checkpointContext = null;
        lastCommitToken = null;
        commitTokenTableReady = false;
        if (openedConnection != null) {
            try {
                openedConnection.rollback();
            } catch (SQLException ignored) {
                // Closing a failed transaction is best effort.
            }
            try {
                openedConnection.close();
            } catch (SQLException exception) {
                throw new IllegalStateException("failed to close JDBC CDC sink", exception);
            }
        }
    }

    private void apply(DataEvent event) throws SQLException {
        switch (event.getOperation()) {
            case INSERT, SNAPSHOT, UPDATE -> upsert(event.getAfter(), event.getKey());
            case DELETE -> delete(event.getBefore(), event.getKey());
            case TRUNCATE -> {
                if (!options.allowTruncate()) {
                    throw new IllegalStateException("CDC truncate event is disabled for JDBC sink");
                }
                try (Statement statement = connection.createStatement()) {
                    statement.executeUpdate("DELETE FROM " + quoteTable(options.table()));
                }
            }
            case SCHEMA_CHANGE -> {
                // Schema DDL is surfaced to a schema-aware sink; JDBC data writes remain transactional.
            }
        }
    }

    private void upsert(Row after, RecordKey key) throws SQLException {
        if (after == null || after.asMap().isEmpty()) {
            throw new IllegalArgumentException("CDC insert/update event has no after image");
        }
        Map<String, Object> keyValues = keyValues(after, key);
        if (exists(keyValues)) {
            List<String> mutableColumns = after.asMap().keySet().stream()
                    .filter(column -> !options.keyColumns().contains(column))
                    .toList();
            if (!mutableColumns.isEmpty()) {
                String assignments = mutableColumns.stream()
                        .map(column -> quote(column) + " = ?")
                        .reduce((left, right) -> left + ", " + right)
                        .orElseThrow();
                String sql =
                        "UPDATE " + quoteTable(options.table()) + " SET " + assignments + " WHERE " + keyPredicate();
                try (PreparedStatement statement = prepare(sql)) {
                    int index = bindColumns(statement, mutableColumns, after);
                    bindKeys(statement, index, keyValues);
                    statement.executeUpdate();
                }
            }
            return;
        }
        List<String> columns = new ArrayList<>(after.asMap().keySet());
        String sql = "INSERT INTO " + quoteTable(options.table()) + " (" + quoteColumns(columns) + ") VALUES ("
                + "?, ".repeat(Math.max(0, columns.size() - 1)) + "?)";
        try (PreparedStatement statement = prepare(sql)) {
            bindColumns(statement, columns, after);
            statement.executeUpdate();
        }
    }

    private void delete(Row before, RecordKey key) throws SQLException {
        Map<String, Object> keyValues = keyValues(before, key);
        String sql = "DELETE FROM " + quoteTable(options.table()) + " WHERE " + keyPredicate();
        try (PreparedStatement statement = prepare(sql)) {
            bindKeys(statement, 1, keyValues);
            statement.executeUpdate();
        }
    }

    private boolean exists(Map<String, Object> keyValues) throws SQLException {
        String sql = "SELECT 1 FROM " + quoteTable(options.table()) + " WHERE " + keyPredicate();
        try (PreparedStatement statement = prepare(sql)) {
            bindKeys(statement, 1, keyValues);
            try (ResultSet result = statement.executeQuery()) {
                return result.next();
            }
        }
    }

    private Map<String, Object> keyValues(Row image, RecordKey key) {
        Map<String, Object> sourceValues = key == null ? Map.of() : key.values();
        Map<String, Object> values = new HashMap<>();
        for (String column : options.keyColumns()) {
            Object value = sourceValues.get(column);
            if (value == null && image != null) {
                value = image.get(column);
            }
            if (value == null) {
                throw new IllegalArgumentException("CDC event does not contain key column '" + column + "'");
            }
            JdbcValueMapper.validateSinkValue(column, value);
            values.put(column, value);
        }
        return values;
    }

    private int bindColumns(PreparedStatement statement, List<String> columns, Row row) throws SQLException {
        int index = 1;
        for (String column : columns) {
            Object value = row.get(column);
            JdbcValueMapper.validateSinkValue(column, value);
            statement.setObject(index++, value);
        }
        return index;
    }

    private void bindKeys(PreparedStatement statement, int startIndex, Map<String, Object> values) throws SQLException {
        int index = startIndex;
        for (String column : options.keyColumns()) {
            statement.setObject(index++, values.get(column));
        }
    }

    private String keyPredicate() {
        return options.keyColumns().stream()
                .map(column -> quote(column) + " = ?")
                .reduce((left, right) -> left + " AND " + right)
                .orElseThrow();
    }

    private PreparedStatement prepare(String sql) throws SQLException {
        PreparedStatement statement = connection.prepareStatement(sql);
        if (options.queryTimeoutSeconds() > 0) {
            statement.setQueryTimeout(options.queryTimeoutSeconds());
        }
        return statement;
    }

    private void ensureCommitTokenTable() throws SQLException {
        if (commitTokenTableReady) {
            return;
        }
        String table = quoteTable(options.commitTokenTable());
        String tokenColumn = quote("commit_token");
        String digestColumn = quote("batch_digest");
        try (Statement statement = connection.createStatement()) {
            statement.execute("CREATE TABLE IF NOT EXISTS " + table + " (" + tokenColumn + " VARCHAR(128) PRIMARY KEY, "
                    + digestColumn + " VARCHAR(128) NOT NULL)");
        }
        connection.commit();
        commitTokenTableReady = true;
    }

    private String findCommitDigest(String token) throws SQLException {
        String sql = "SELECT " + quote("batch_digest") + " FROM " + quoteTable(options.commitTokenTable()) + " WHERE "
                + quote("commit_token") + " = ?";
        try (PreparedStatement statement = connection.prepareStatement(sql)) {
            statement.setString(1, token);
            try (ResultSet result = statement.executeQuery()) {
                return result.next() ? result.getString(1) : null;
            }
        }
    }

    private void insertCommitMarker(SinkCommitContext context) throws SQLException {
        String sql = "INSERT INTO " + quoteTable(options.commitTokenTable()) + " ("
                + quoteColumns(List.of("commit_token", "batch_digest")) + ") VALUES (?, ?)";
        try (PreparedStatement statement = connection.prepareStatement(sql)) {
            statement.setString(1, context.commitToken());
            statement.setString(2, context.batchDigest());
            statement.executeUpdate();
        }
    }

    private String quoteTable(String table) throws SQLException {
        return JdbcIdentifiers.quoteTable(connection.getMetaData(), table);
    }

    private String quoteColumns(List<String> columns) throws SQLException {
        return JdbcIdentifiers.quoteColumns(connection.getMetaData(), columns);
    }

    private String quote(String column) {
        try {
            return JdbcIdentifiers.quoteColumns(connection.getMetaData(), List.of(column));
        } catch (SQLException exception) {
            throw new IllegalStateException("failed to quote JDBC CDC column", exception);
        }
    }

    private void validateCommitContext(SinkCommitContext context) {
        Objects.requireNonNull(context, "commitContext must not be null");
        if (!checkpointContext.jobId().equals(context.jobId())
                || !checkpointContext.splitId().equals(context.splitId())) {
            throw new IllegalArgumentException("CDC commit context does not match checkpoint context");
        }
        checkpointContext.assertCurrent();
    }

    private void rollback(RuntimeException failure) {
        try {
            connection.rollback();
        } catch (SQLException exception) {
            failure.addSuppressed(exception);
        }
    }

    private static void closeQuietly(Connection openedConnection, Throwable failure) {
        if (openedConnection == null) {
            return;
        }
        try {
            openedConnection.close();
        } catch (SQLException exception) {
            failure.addSuppressed(exception);
        }
    }

    private void requireOpen(String operation) {
        if (state != State.OPEN) {
            throw new IllegalStateException("cannot " + operation + " JDBC CDC sink while state is " + state);
        }
    }

    private enum State {
        NEW,
        OPEN,
        CLOSED
    }
}
