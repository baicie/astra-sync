package io.astrasync.connector.jdbc;

import io.astrasync.connector.api.ConnectorConfiguration;
import java.util.HashSet;
import java.util.Objects;
import java.util.Set;
import java.util.regex.Pattern;

record JdbcRangeSplitOptions(
        String url,
        String user,
        String password,
        String table,
        String splitColumn,
        int splitCount,
        int fetchSize,
        int queryTimeoutSeconds) {
    private static final String URL = "url";
    private static final String USER = "user";
    private static final String PASSWORD = "password";
    private static final String TABLE = "table";
    private static final String SPLIT_COLUMN = "splitColumn";
    private static final String SPLIT_COUNT = "splitCount";
    private static final String FETCH_SIZE = "fetchSize";
    private static final String QUERY_TIMEOUT = "queryTimeoutSeconds";
    private static final Pattern COLUMN_PATTERN = Pattern.compile("[A-Za-z_][A-Za-z0-9_]*");

    static JdbcRangeSplitOptions from(ConnectorConfiguration configuration) {
        Objects.requireNonNull(configuration, "configuration must not be null");
        rejectUnknown(configuration);
        String table = JdbcConnectorOptions.validateTable(required(configuration, TABLE));
        String splitColumn = required(configuration, SPLIT_COLUMN);
        if (!COLUMN_PATTERN.matcher(splitColumn).matches()) {
            throw new IllegalArgumentException("connector option 'splitColumn' must be one unquoted SQL identifier");
        }
        int splitCount = positive(configuration, SPLIT_COUNT, 1);
        int fetchSize = positive(configuration, FETCH_SIZE, 0);
        int queryTimeoutSeconds = positive(configuration, QUERY_TIMEOUT, 0);
        return new JdbcRangeSplitOptions(
                required(configuration, URL),
                optional(configuration, USER),
                optional(configuration, PASSWORD),
                table,
                splitColumn,
                splitCount,
                fetchSize,
                queryTimeoutSeconds);
    }

    private static String required(ConnectorConfiguration configuration, String key) {
        String value = configuration.required(key);
        if (value.isBlank()) {
            throw new IllegalArgumentException("connector option '" + key + "' must not be blank");
        }
        return value;
    }

    private static String optional(ConnectorConfiguration configuration, String key) {
        return configuration.optional(key).orElse(null);
    }

    private static int positive(ConnectorConfiguration configuration, String key, int defaultValue) {
        if (!configuration.contains(key)) {
            return defaultValue;
        }
        int value = configuration.getInt(key);
        if (value <= 0) {
            throw new IllegalArgumentException("connector option '" + key + "' must be a positive integer");
        }
        return value;
    }

    private static void rejectUnknown(ConnectorConfiguration configuration) {
        Set<String> unknown = new HashSet<>(configuration.asMap().keySet());
        unknown.removeAll(Set.of(URL, USER, PASSWORD, TABLE, SPLIT_COLUMN, SPLIT_COUNT, FETCH_SIZE, QUERY_TIMEOUT));
        if (!unknown.isEmpty()) {
            throw new IllegalArgumentException(
                    "unknown JDBC split option '" + unknown.iterator().next() + "'");
        }
    }
}
