package io.astrasync.connector.jdbc;

import io.astrasync.connector.api.CheckpointContext;
import io.astrasync.connector.api.SinkCommitContext;
import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.IdempotentBatchSink;
import java.sql.Connection;
import java.sql.DatabaseMetaData;
import java.sql.PreparedStatement;
import java.sql.SQLException;
import java.sql.Statement;
import java.util.List;
import java.util.Objects;
import java.util.Set;
import java.util.TreeSet;

final class JdbcBatchSink implements IdempotentBatchSink {
    private final JdbcConnectorOptions options;

    private State state = State.NEW;
    private Connection connection;
    private PreparedStatement statement;
    private List<String> columns;
    private Set<String> columnSet;
    private CheckpointContext checkpointContext;
    private long commitSequence;
    private String lastCommitToken;
    private boolean commitTokenTableReady;

    JdbcBatchSink(JdbcConnectorOptions options) {
        this.options = options;
    }

    @Override
    public void open() {
        openInternal(null);
    }

    @Override
    public void open(CheckpointContext context) {
        checkpointContext = Objects.requireNonNull(context, "context must not be null");
        openInternal(context);
    }

    private void openInternal(CheckpointContext context) {
        requireState(State.NEW, "open");
        Connection openedConnection = null;
        try {
            openedConnection = options.connect();
            openedConnection.setAutoCommit(false);
            connection = openedConnection;
            state = State.OPEN;
        } catch (SQLException | RuntimeException exception) {
            state = State.CLOSED;
            closeFailedOpen(openedConnection, exception);
            throw new IllegalStateException("failed to open JDBC sink", exception);
        }
    }

    @Override
    public void writeBatch(RowBatch batch) {
        writeBatchInternal(batch, null);
    }

    @Override
    public void writeBatch(RowBatch batch, SinkCommitContext commitContext) {
        writeBatchInternal(batch, Objects.requireNonNull(commitContext, "commitContext must not be null"));
    }

    private void writeBatchInternal(RowBatch batch, SinkCommitContext commitContext) {
        requireState(State.OPEN, "write");
        Objects.requireNonNull(batch, "batch must not be null");
        if (batch.rows().isEmpty()) {
            return;
        }

        RuntimeException failure = null;
        try {
            if (commitContext != null) {
                validateCommitContext(commitContext);
                ensureCommitTokenTable();
                String recordedDigest = findCommitDigest(commitContext.commitToken());
                if (recordedDigest != null) {
                    if (!recordedDigest.equals(commitContext.batchDigest())) {
                        throw new IllegalStateException(
                                "commit token is already associated with a different batch digest");
                    }
                    connection.commit();
                    lastCommitToken = commitContext.commitToken();
                    return;
                }
            }
            if (statement == null) {
                establishStatement(batch.rows().get(0));
            }
            statement.clearBatch();
            for (Row row : batch.rows()) {
                validateColumns(row);
                bindRow(row);
                statement.addBatch();
            }
            int[] results = statement.executeBatch();
            for (int result : results) {
                if (result == Statement.EXECUTE_FAILED) {
                    throw new SQLException("JDBC driver reported an executeBatch failure");
                }
            }
            if (commitContext != null) {
                insertCommitMarker(commitContext);
            }
            if (checkpointContext != null) {
                assertEpoch(checkpointContext.executionEpoch());
            }
            connection.commit();
            if (commitContext != null) {
                lastCommitToken = commitContext.commitToken();
            } else if (checkpointContext != null) {
                commitSequence++;
                lastCommitToken =
                        checkpointContext.executionEpoch() + ":" + checkpointContext.splitId() + ":" + commitSequence;
            }
        } catch (IllegalArgumentException exception) {
            failure = exception;
            rollbackAfterFailure(failure);
            throw failure;
        } catch (SQLException exception) {
            failure = new IllegalStateException("failed to write JDBC sink", exception);
            rollbackAfterFailure(failure);
            throw failure;
        } finally {
            clearBatch(failure);
        }
    }

    @Override
    public void assertEpoch(long executionEpoch) {
        if (checkpointContext == null) {
            throw new IllegalStateException("JDBC sink is not open for checkpoint execution");
        }
        if (checkpointContext.executionEpoch() != executionEpoch) {
            throw new IllegalStateException(
                    "sink epoch " + executionEpoch + " does not match " + checkpointContext.executionEpoch());
        }
        checkpointContext.assertCurrent();
    }

    @Override
    public String lastCommitToken() {
        if (checkpointContext == null || (commitSequence <= 0 && lastCommitToken == null)) {
            throw new IllegalStateException("JDBC sink has not committed a checkpoint batch");
        }
        return lastCommitToken;
    }

    @Override
    public void close() {
        if (state == State.CLOSED) {
            return;
        }
        state = State.CLOSED;
        PreparedStatement openedStatement = statement;
        Connection openedConnection = connection;
        statement = null;
        connection = null;
        columns = null;
        columnSet = null;
        checkpointContext = null;
        commitSequence = 0;
        lastCommitToken = null;
        commitTokenTableReady = false;

        RuntimeException failure = null;
        if (openedConnection != null) {
            try {
                openedConnection.rollback();
            } catch (SQLException exception) {
                failure = new IllegalStateException("failed to rollback JDBC sink transaction", exception);
            }
        }
        failure = closeResource(openedStatement, "JDBC sink statement", failure);
        failure = closeResource(openedConnection, "JDBC sink connection", failure);
        if (failure != null) {
            throw failure;
        }
    }

    private void validateCommitContext(SinkCommitContext commitContext) {
        if (checkpointContext == null) {
            throw new IllegalStateException("JDBC sink is not open for checkpoint execution");
        }
        if (!checkpointContext.jobId().equals(commitContext.jobId())
                || !checkpointContext.splitId().equals(commitContext.splitId())) {
            throw new IllegalArgumentException("commit context does not match the JDBC checkpoint context");
        }
        checkpointContext.assertCurrent();
    }

    private void ensureCommitTokenTable() throws SQLException {
        if (commitTokenTableReady) {
            return;
        }
        DatabaseMetaData metadata = connection.getMetaData();
        String table = JdbcIdentifiers.quoteTable(metadata, options.commitTokenTable());
        String tokenColumn = JdbcIdentifiers.quoteColumns(metadata, List.of("commit_token"));
        String digestColumn = JdbcIdentifiers.quoteColumns(metadata, List.of("batch_digest"));
        try (Statement create = connection.createStatement()) {
            create.execute("CREATE TABLE IF NOT EXISTS " + table + " (" + tokenColumn + " VARCHAR(128) PRIMARY KEY, "
                    + digestColumn + " VARCHAR(128) NOT NULL)");
        }
        connection.commit();
        commitTokenTableReady = true;
    }

    private String findCommitDigest(String commitToken) throws SQLException {
        DatabaseMetaData metadata = connection.getMetaData();
        String table = JdbcIdentifiers.quoteTable(metadata, options.commitTokenTable());
        String digestColumn = JdbcIdentifiers.quoteColumns(metadata, List.of("batch_digest"));
        String tokenColumn = JdbcIdentifiers.quoteColumns(metadata, List.of("commit_token"));
        try (PreparedStatement lookup = connection.prepareStatement(
                "SELECT " + digestColumn + " FROM " + table + " WHERE " + tokenColumn + " = ?")) {
            lookup.setString(1, commitToken);
            try (var result = lookup.executeQuery()) {
                return result.next() ? result.getString(1) : null;
            }
        }
    }

    private void insertCommitMarker(SinkCommitContext commitContext) throws SQLException {
        DatabaseMetaData metadata = connection.getMetaData();
        String table = JdbcIdentifiers.quoteTable(metadata, options.commitTokenTable());
        String tokenColumn = JdbcIdentifiers.quoteColumns(metadata, List.of("commit_token", "batch_digest"));
        try (PreparedStatement marker =
                connection.prepareStatement("INSERT INTO " + table + " (" + tokenColumn + ") VALUES (?, ?)")) {
            marker.setString(1, commitContext.commitToken());
            marker.setString(2, commitContext.batchDigest());
            marker.executeUpdate();
        }
    }

    private void establishStatement(Row row) throws SQLException {
        if (row.size() == 0) {
            throw new IllegalArgumentException("JDBC sink cannot derive columns from an empty Row");
        }
        columns = List.copyOf(row.asMap().keySet());
        columnSet = Set.copyOf(columns);
        DatabaseMetaData metadata = connection.getMetaData();
        String sql = "INSERT INTO " + JdbcIdentifiers.quoteTable(metadata, options.table()) + " ("
                + JdbcIdentifiers.quoteColumns(metadata, columns) + ") VALUES ("
                + "?, ".repeat(Math.max(0, columns.size() - 1)) + "?)";
        statement = connection.prepareStatement(sql);
        if (options.queryTimeoutSeconds() > 0) {
            statement.setQueryTimeout(options.queryTimeoutSeconds());
        }
    }

    private void validateColumns(Row row) {
        Set<String> actual = new TreeSet<>(row.asMap().keySet());
        if (!actual.equals(new TreeSet<>(columnSet))) {
            throw new IllegalArgumentException("JDBC sink row columns must match " + columns + "; received " + actual);
        }
    }

    private void bindRow(Row row) throws SQLException {
        for (int index = 0; index < columns.size(); index++) {
            String column = columns.get(index);
            Object value = row.get(column);
            JdbcValueMapper.validateSinkValue(column, value);
            statement.setObject(index + 1, value);
        }
    }

    private void rollbackAfterFailure(RuntimeException failure) {
        try {
            connection.rollback();
        } catch (SQLException rollbackFailure) {
            failure.addSuppressed(rollbackFailure);
        }
    }

    private void clearBatch(RuntimeException failure) {
        if (statement == null) {
            return;
        }
        try {
            statement.clearBatch();
        } catch (SQLException clearFailure) {
            if (failure != null) {
                failure.addSuppressed(clearFailure);
            } else {
                throw new IllegalStateException("failed to clear JDBC sink batch", clearFailure);
            }
        }
    }

    private static void closeFailedOpen(Connection openedConnection, Throwable failure) {
        if (openedConnection == null) {
            return;
        }
        try {
            openedConnection.close();
        } catch (SQLException closeFailure) {
            failure.addSuppressed(closeFailure);
        }
    }

    private static RuntimeException closeResource(AutoCloseable resource, String label, RuntimeException failure) {
        if (resource == null) {
            return failure;
        }
        try {
            resource.close();
            return failure;
        } catch (Exception exception) {
            RuntimeException next = failure == null ? new IllegalStateException(label, exception) : failure;
            if (failure != null) {
                failure.addSuppressed(exception);
            }
            return next;
        }
    }

    private void requireState(State expected, String operation) {
        if (state != expected) {
            throw new IllegalStateException("cannot " + operation + " JDBC sink while state is " + state);
        }
    }

    private enum State {
        NEW,
        OPEN,
        CLOSED
    }
}
