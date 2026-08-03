package io.astrasync.connector.jdbc;

import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import java.sql.Connection;
import java.sql.DatabaseMetaData;
import java.sql.PreparedStatement;
import java.sql.SQLException;
import java.sql.Statement;
import java.util.List;
import java.util.Objects;
import java.util.Set;
import java.util.TreeSet;

final class JdbcBatchSink implements BatchSink {
    private final JdbcConnectorOptions options;

    private State state = State.NEW;
    private Connection connection;
    private PreparedStatement statement;
    private List<String> columns;
    private Set<String> columnSet;

    JdbcBatchSink(JdbcConnectorOptions options) {
        this.options = options;
    }

    @Override
    public void open() {
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
        requireState(State.OPEN, "write");
        Objects.requireNonNull(batch, "batch must not be null");
        if (batch.rows().isEmpty()) {
            return;
        }

        RuntimeException failure = null;
        try {
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
            connection.commit();
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
