package io.astrasync.connector.jdbc;

import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.source.CheckpointableBatchSource;
import io.astrasync.connector.api.source.SplitPosition;
import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.sql.ResultSetMetaData;
import java.sql.SQLException;
import java.util.ArrayList;
import java.util.List;
import java.util.Objects;

final class JdbcBatchSource implements CheckpointableBatchSource {
    private final JdbcConnectorOptions options;

    private State state = State.NEW;
    private Connection connection;
    private PreparedStatement statement;
    private ResultSet resultSet;
    private List<JdbcValueMapper.Column> columns = List.of();
    private boolean columnTypesValidated;
    private long recordNumber;

    JdbcBatchSource(JdbcConnectorOptions options) {
        this.options = options;
    }

    @Override
    public void open() {
        openAt(SplitPosition.unbounded());
    }

    @Override
    public void openAt(SplitPosition resumePosition) {
        requireState(State.NEW, "open");
        validateResumePosition(Objects.requireNonNull(resumePosition, "resumePosition must not be null"));
        Connection openedConnection = null;
        PreparedStatement openedStatement = null;
        ResultSet openedResultSet = null;
        try {
            openedConnection = options.connect();
            openedConnection.setReadOnly(true);
            openedConnection.setAutoCommit(false);
            openedStatement = openedConnection.prepareStatement(
                    options.query(), ResultSet.TYPE_FORWARD_ONLY, ResultSet.CONCUR_READ_ONLY);
            if (options.resumeValue() != null) {
                openedStatement.setString(1, options.resumeValue());
            }
            if (options.fetchSize() > 0) {
                openedStatement.setFetchSize(options.fetchSize());
            }
            if (options.queryTimeoutSeconds() > 0) {
                openedStatement.setQueryTimeout(options.queryTimeoutSeconds());
            }
            openedResultSet = openedStatement.executeQuery();
            ResultSetMetaData metadata = openedResultSet.getMetaData();
            List<JdbcValueMapper.Column> discoveredColumns = JdbcValueMapper.columns(metadata);

            connection = openedConnection;
            statement = openedStatement;
            resultSet = openedResultSet;
            columns = discoveredColumns;
            state = State.OPEN;
        } catch (SQLException | RuntimeException exception) {
            state = State.CLOSED;
            closeFailedOpen(openedResultSet, openedStatement, openedConnection, exception);
            throw new IllegalStateException("failed to open JDBC source", exception);
        }
    }

    @Override
    public RowBatch readBatch(int maxRows) {
        requireState(State.OPEN, "read");
        if (maxRows <= 0) {
            throw new IllegalArgumentException("maxRows must be positive");
        }
        validateColumnTypes();

        List<Row> rows = new ArrayList<>(Math.min(maxRows, 1_024));
        while (rows.size() < maxRows) {
            boolean hasNext;
            try {
                hasNext = resultSet.next();
            } catch (SQLException exception) {
                throw new IllegalStateException(
                        "failed to read JDBC source at record " + (recordNumber + 1), exception);
            }
            if (!hasNext) {
                state = State.ENDED;
                return RowBatch.last(rows);
            }

            recordNumber++;
            try {
                rows.add(JdbcValueMapper.readRow(resultSet, columns));
            } catch (SQLException | RuntimeException exception) {
                throw new IllegalStateException(
                        "failed to read JDBC source at record " + recordNumber + ": " + exception.getMessage(),
                        exception);
            }
        }
        return RowBatch.data(rows);
    }

    private void validateResumePosition(SplitPosition position) {
        if (position.isUnbounded()) {
            if (options.resumeValue() != null) {
                throw new IllegalArgumentException("configured JDBC resume value requires a bounded resume position");
            }
            return;
        }
        if (options.resumeColumn() == null || options.resumeValue() == null) {
            throw new IllegalArgumentException("bounded JDBC resume position was not applied to the source query");
        }
        if (position.offsets().size() != 1
                || !options.resumeValue().equals(position.offsets().get(options.resumeColumn()))) {
            throw new IllegalArgumentException("JDBC resume position does not match the materialized source query");
        }
    }

    @Override
    public SplitPosition positionAfter(RowBatch batch) {
        Objects.requireNonNull(batch, "batch must not be null");
        if (options.resumeColumn() == null) {
            throw new IllegalStateException("JDBC source has no configured resumeColumn");
        }
        if (batch.rows().isEmpty()) {
            return SplitPosition.unbounded();
        }
        Object value = batch.rows().get(batch.rows().size() - 1).get(options.resumeColumn());
        if (value == null) {
            throw new IllegalStateException("JDBC checkpoint resume column is null: " + options.resumeColumn());
        }
        return new SplitPosition(java.util.Map.of(options.resumeColumn(), value.toString()));
    }

    @Override
    public void close() {
        if (state == State.CLOSED) {
            return;
        }
        state = State.CLOSED;
        ResultSet openedResultSet = resultSet;
        PreparedStatement openedStatement = statement;
        Connection openedConnection = connection;
        resultSet = null;
        statement = null;
        connection = null;
        columns = List.of();
        columnTypesValidated = false;

        RuntimeException failure = null;
        failure = closeResource(openedResultSet, "JDBC source result set", failure);
        failure = closeResource(openedStatement, "JDBC source statement", failure);
        if (openedConnection != null) {
            try {
                openedConnection.rollback();
            } catch (SQLException exception) {
                failure = addFailure(failure, "failed to rollback JDBC source transaction", exception);
            }
        }
        failure = closeResource(openedConnection, "JDBC source connection", failure);
        if (failure != null) {
            throw failure;
        }
    }

    private static void closeFailedOpen(
            ResultSet openedResultSet,
            PreparedStatement openedStatement,
            Connection openedConnection,
            Throwable failure) {
        addSuppressed(failure, openedResultSet);
        addSuppressed(failure, openedStatement);
        if (openedConnection != null) {
            try {
                openedConnection.rollback();
            } catch (SQLException exception) {
                failure.addSuppressed(exception);
            }
        }
        addSuppressed(failure, openedConnection);
    }

    private static RuntimeException closeResource(AutoCloseable resource, String label, RuntimeException failure) {
        if (resource == null) {
            return failure;
        }
        try {
            resource.close();
            return failure;
        } catch (Exception exception) {
            return addFailure(failure, label, exception);
        }
    }

    private static RuntimeException addFailure(RuntimeException failure, String message, Throwable cause) {
        RuntimeException next = failure == null ? new IllegalStateException(message, cause) : failure;
        if (failure != null) {
            failure.addSuppressed(cause);
        }
        return next;
    }

    private static void addSuppressed(Throwable failure, AutoCloseable resource) {
        if (resource == null) {
            return;
        }
        try {
            resource.close();
        } catch (Exception exception) {
            failure.addSuppressed(exception);
        }
    }

    private void validateColumnTypes() {
        if (columnTypesValidated) {
            return;
        }
        try {
            JdbcValueMapper.validateSourceColumns(columns);
            columnTypesValidated = true;
        } catch (RuntimeException exception) {
            throw new IllegalStateException(
                    "failed to read JDBC source at record " + (recordNumber + 1) + ": " + exception.getMessage(),
                    exception);
        }
    }

    private void requireState(State expected, String operation) {
        if (state != expected) {
            throw new IllegalStateException("cannot " + operation + " JDBC source while state is " + state);
        }
    }

    private enum State {
        NEW,
        OPEN,
        ENDED,
        CLOSED
    }
}
