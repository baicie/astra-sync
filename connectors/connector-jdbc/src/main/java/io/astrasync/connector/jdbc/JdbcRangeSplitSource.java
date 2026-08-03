package io.astrasync.connector.jdbc;

import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.connector.api.source.SplitSource;
import java.math.BigDecimal;
import java.math.BigInteger;
import java.sql.Connection;
import java.sql.PreparedStatement;
import java.sql.ResultSet;
import java.sql.SQLException;
import java.util.ArrayList;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;

/** Enumerates numeric JDBC table ranges and creates one bounded reader per range. */
public final class JdbcRangeSplitSource implements SplitSource {
    private static final BigInteger ONE = BigInteger.ONE;

    private final JdbcRangeSplitOptions options;
    private final String sourceId;

    public JdbcRangeSplitSource(ConnectorConfiguration configuration) {
        this.options = JdbcRangeSplitOptions.from(configuration);
        this.sourceId = "jdbc:" + options.table();
    }

    @Override
    public List<SourceSplit> enumerate() {
        try (Connection connection = JdbcConnectorOptions.connect(options.url(), options.user(), options.password())) {
            connection.setReadOnly(true);
            String table = JdbcIdentifiers.quoteTable(connection.getMetaData(), options.table());
            String column = JdbcIdentifiers.quoteColumns(connection.getMetaData(), List.of(options.splitColumn()));
            try (PreparedStatement statement = connection.prepareStatement(
                            "SELECT MIN(" + column + "), MAX(" + column + ") FROM " + table);
                    ResultSet resultSet = statement.executeQuery()) {
                if (!resultSet.next() || resultSet.getObject(1) == null || resultSet.getObject(2) == null) {
                    return List.of();
                }
                BigInteger minimum = integral(resultSet.getBigDecimal(1), "minimum");
                BigInteger maximum = integral(resultSet.getBigDecimal(2), "maximum");
                if (minimum.compareTo(maximum) > 0) {
                    throw new IllegalStateException("JDBC split minimum exceeds maximum");
                }
                return ranges(minimum, maximum);
            }
        } catch (SQLException | RuntimeException exception) {
            if (exception instanceof IllegalStateException stateException
                    && stateException.getMessage() != null
                    && stateException.getMessage().startsWith("JDBC split")) {
                throw stateException;
            }
            throw new IllegalStateException("failed to enumerate JDBC source splits", exception);
        }
    }

    @Override
    public BatchSource createSource(SourceSplit split) {
        Objects.requireNonNull(split, "split must not be null");
        if (!sourceId.equals(split.sourceId())) {
            throw new IllegalArgumentException("split belongs to source '" + split.sourceId() + "'");
        }
        BigInteger start = boundary(split.start(), false);
        BigInteger end = boundary(split.end(), true);
        if (start == null && end == null) {
            throw new IllegalArgumentException("JDBC split must have at least one bounded boundary");
        }
        if (start != null && end != null && start.compareTo(end) >= 0) {
            throw new IllegalArgumentException("split start must be less than split end");
        }

        StringBuilder query = new StringBuilder("SELECT * FROM ")
                .append(options.table())
                .append(" WHERE ")
                .append(options.splitColumn());
        if (start != null) {
            query.append(" >= ").append(start);
        }
        if (end != null) {
            if (start != null) {
                query.append(" AND ");
            }
            query.append(options.splitColumn()).append(" < ").append(end);
        }
        query.append(" ORDER BY ").append(options.splitColumn());

        Map<String, String> sourceOptions = new LinkedHashMap<>();
        sourceOptions.put("url", options.url());
        sourceOptions.put("query", query.toString());
        if (options.user() != null) {
            sourceOptions.put("user", options.user());
        }
        if (options.password() != null) {
            sourceOptions.put("password", options.password());
        }
        if (options.fetchSize() > 0) {
            sourceOptions.put("fetchSize", Integer.toString(options.fetchSize()));
        }
        if (options.queryTimeoutSeconds() > 0) {
            sourceOptions.put("queryTimeoutSeconds", Integer.toString(options.queryTimeoutSeconds()));
        }
        return new JdbcConnectorFactory().createSource(ConnectorConfiguration.of(sourceOptions));
    }

    private List<SourceSplit> ranges(BigInteger minimum, BigInteger maximum) {
        BigInteger width = maximum.subtract(minimum).add(ONE);
        BigInteger requested = BigInteger.valueOf(options.splitCount());
        int count = width.compareTo(requested) < 0 ? width.intValueExact() : options.splitCount();
        List<SourceSplit> splits = new ArrayList<>(count);
        for (int index = 0; index < count; index++) {
            BigInteger start =
                    minimum.add(width.multiply(BigInteger.valueOf(index)).divide(BigInteger.valueOf(count)));
            BigInteger end = index == count - 1
                    ? null
                    : minimum.add(width.multiply(BigInteger.valueOf(index + 1)).divide(BigInteger.valueOf(count)));
            SplitPosition startPosition = position(start);
            SplitPosition endPosition = end == null ? SplitPosition.unbounded() : position(end);
            splits.add(new SourceSplit(sourceId + "-split-" + index, sourceId, startPosition, endPosition));
        }
        return List.copyOf(splits);
    }

    private SplitPosition position(BigInteger value) {
        return new SplitPosition(Map.of(options.splitColumn(), value.toString()));
    }

    private BigInteger boundary(SplitPosition position, boolean end) {
        if (position.isUnbounded()) {
            return null;
        }
        if (position.offsets().size() != 1 || !position.offsets().containsKey(options.splitColumn())) {
            throw new IllegalArgumentException(
                    "JDBC split " + (end ? "end" : "start") + " must contain only '" + options.splitColumn() + "'");
        }
        try {
            return new BigInteger(position.offsets().get(options.splitColumn()));
        } catch (NumberFormatException exception) {
            throw new IllegalArgumentException("JDBC split boundary must be an integer", exception);
        }
    }

    private static BigInteger integral(BigDecimal value, String boundary) {
        try {
            return value.toBigIntegerExact();
        } catch (ArithmeticException exception) {
            throw new IllegalStateException("JDBC split " + boundary + " is not an integer", exception);
        }
    }
}
