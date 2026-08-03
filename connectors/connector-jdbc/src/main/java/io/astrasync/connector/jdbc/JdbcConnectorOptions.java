package io.astrasync.connector.jdbc;

import io.astrasync.connector.api.ConnectorConfiguration;
import java.sql.Connection;
import java.sql.DriverManager;
import java.sql.SQLException;
import java.util.HashSet;
import java.util.Objects;
import java.util.Properties;
import java.util.Set;
import java.util.regex.Pattern;

record JdbcConnectorOptions(
        String url, String user, String password, String query, String table, int fetchSize, int queryTimeoutSeconds) {
    private static final String URL = "url";
    private static final String USER = "user";
    private static final String PASSWORD = "password";
    private static final String QUERY = "query";
    private static final String TABLE = "table";
    private static final String FETCH_SIZE = "fetchSize";
    private static final String QUERY_TIMEOUT = "queryTimeoutSeconds";
    private static final Pattern TABLE_PATTERN = Pattern.compile("[A-Za-z_][A-Za-z0-9_]*(\\.[A-Za-z_][A-Za-z0-9_]*)?");

    JdbcConnectorOptions {
        url = requireNonBlank(url, URL);
        if (!url.startsWith("jdbc:")) {
            throw new IllegalArgumentException("connector option 'url' must start with 'jdbc:'");
        }
        if (query == null && table == null) {
            throw new IllegalArgumentException("one of query or table must be configured");
        }
        if (query != null && table != null) {
            throw new IllegalArgumentException("query and table cannot be configured together");
        }
        if (fetchSize < 0 || queryTimeoutSeconds < 0) {
            throw new IllegalArgumentException("JDBC numeric options must not be negative");
        }
    }

    static JdbcConnectorOptions source(ConnectorConfiguration configuration) {
        Objects.requireNonNull(configuration, "configuration must not be null");
        rejectUnknown(configuration, Set.of(URL, USER, PASSWORD, QUERY, FETCH_SIZE, QUERY_TIMEOUT));
        String query = requiredNonBlank(configuration, QUERY);
        return new JdbcConnectorOptions(
                requiredNonBlank(configuration, URL),
                optional(configuration, USER),
                optional(configuration, PASSWORD),
                query,
                null,
                positiveOrDefault(configuration, FETCH_SIZE),
                positiveOrDefault(configuration, QUERY_TIMEOUT));
    }

    static JdbcConnectorOptions sink(ConnectorConfiguration configuration) {
        Objects.requireNonNull(configuration, "configuration must not be null");
        rejectUnknown(configuration, Set.of(URL, USER, PASSWORD, TABLE, QUERY_TIMEOUT));
        String table = requiredNonBlank(configuration, TABLE);
        if (!TABLE_PATTERN.matcher(table).matches()) {
            throw new IllegalArgumentException(
                    "connector option 'table' must be one or two unquoted SQL identifier segments");
        }
        return new JdbcConnectorOptions(
                requiredNonBlank(configuration, URL),
                optional(configuration, USER),
                optional(configuration, PASSWORD),
                null,
                table,
                0,
                positiveOrDefault(configuration, QUERY_TIMEOUT));
    }

    Connection connect() throws SQLException {
        Properties properties = new Properties();
        if (user != null) {
            properties.setProperty(USER, user);
        }
        if (password != null) {
            properties.setProperty(PASSWORD, password);
        }
        return properties.isEmpty() ? DriverManager.getConnection(url) : DriverManager.getConnection(url, properties);
    }

    @Override
    public String toString() {
        return "JdbcConnectorOptions{keys=[url, "
                + (query == null ? "table" : "query")
                + (user == null ? "" : ", user")
                + (password == null ? "" : ", password")
                + (fetchSize == 0 ? "" : ", fetchSize")
                + (queryTimeoutSeconds == 0 ? "" : ", queryTimeoutSeconds")
                + "]}";
    }

    private static String requiredNonBlank(ConnectorConfiguration configuration, String key) {
        return requireNonBlank(configuration.required(key), key);
    }

    private static String requireNonBlank(String value, String key) {
        Objects.requireNonNull(value, "connector option '" + key + "' must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException("connector option '" + key + "' must not be blank");
        }
        return value;
    }

    private static String optional(ConnectorConfiguration configuration, String key) {
        return configuration.optional(key).orElse(null);
    }

    private static int positiveOrDefault(ConnectorConfiguration configuration, String key) {
        if (!configuration.contains(key)) {
            return 0;
        }
        int value = configuration.getInt(key);
        if (value <= 0) {
            throw new IllegalArgumentException("connector option '" + key + "' must be a positive integer");
        }
        return value;
    }

    private static void rejectUnknown(ConnectorConfiguration configuration, Set<String> allowed) {
        Set<String> unknown = new HashSet<>(configuration.asMap().keySet());
        unknown.removeAll(allowed);
        if (!unknown.isEmpty()) {
            throw new IllegalArgumentException(
                    "unknown JDBC connector option '" + unknown.iterator().next() + "'");
        }
    }
}
