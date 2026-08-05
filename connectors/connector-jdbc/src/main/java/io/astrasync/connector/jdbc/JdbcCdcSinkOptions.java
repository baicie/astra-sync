package io.astrasync.connector.jdbc;

import io.astrasync.connector.api.ConnectorConfiguration;
import java.util.Arrays;
import java.util.HashSet;
import java.util.List;
import java.util.Objects;
import java.util.Set;
import java.util.regex.Pattern;

record JdbcCdcSinkOptions(
        String url,
        String user,
        String password,
        String table,
        String commitTokenTable,
        List<String> keyColumns,
        boolean allowTruncate,
        int queryTimeoutSeconds) {
    private static final Set<String> ALLOWED = Set.of(
            "url",
            "user",
            "password",
            "table",
            "commitTokenTable",
            "keyColumns",
            "allowTruncate",
            "queryTimeoutSeconds");
    private static final Pattern COLUMN_PATTERN = Pattern.compile("[A-Za-z_][A-Za-z0-9_]*");

    JdbcCdcSinkOptions {
        url = requireText(url, "url");
        if (!url.startsWith("jdbc:")) {
            throw new IllegalArgumentException("connector option 'url' must start with 'jdbc:'");
        }
        table = JdbcConnectorOptions.validateTable(requireText(table, "table"));
        commitTokenTable = JdbcConnectorOptions.validateTable(requireText(commitTokenTable, "commitTokenTable"));
        keyColumns = validateKeyColumns(keyColumns);
        if (queryTimeoutSeconds < 0) {
            throw new IllegalArgumentException("queryTimeoutSeconds must not be negative");
        }
    }

    static JdbcCdcSinkOptions from(ConnectorConfiguration configuration) {
        Objects.requireNonNull(configuration, "configuration must not be null");
        Set<String> unknown = new HashSet<>(configuration.asMap().keySet());
        unknown.removeAll(ALLOWED);
        if (!unknown.isEmpty()) {
            throw new IllegalArgumentException(
                    "unknown JDBC CDC sink option '" + unknown.iterator().next() + "'");
        }
        String table = required(configuration, "table");
        String commitTokenTable =
                configuration.optional("commitTokenTable").orElse(JdbcConnectorOptions.defaultCommitTokenTable(table));
        String keyValue = required(configuration, "keyColumns");
        List<String> keyColumns =
                Arrays.stream(keyValue.split(",", -1)).map(String::trim).toList();
        return new JdbcCdcSinkOptions(
                required(configuration, "url"),
                optional(configuration, "user"),
                optional(configuration, "password"),
                table,
                commitTokenTable,
                keyColumns,
                configuration.getBoolean("allowTruncate", false),
                positiveOrZero(configuration, "queryTimeoutSeconds"));
    }

    private static List<String> validateKeyColumns(List<String> columns) {
        Objects.requireNonNull(columns, "keyColumns must not be null");
        if (columns.isEmpty()) {
            throw new IllegalArgumentException("keyColumns must not be empty");
        }
        Set<String> unique = new HashSet<>();
        for (String column : columns) {
            if (column == null || !COLUMN_PATTERN.matcher(column).matches() || !unique.add(column)) {
                throw new IllegalArgumentException("keyColumns must contain unique SQL identifiers");
            }
        }
        return List.copyOf(columns);
    }

    private static String required(ConnectorConfiguration configuration, String key) {
        String value = configuration.required(key);
        return requireText(value, key);
    }

    private static String optional(ConnectorConfiguration configuration, String key) {
        return configuration.optional(key).filter(value -> !value.isBlank()).orElse(null);
    }

    private static int positiveOrZero(ConnectorConfiguration configuration, String key) {
        if (!configuration.contains(key)) {
            return 0;
        }
        int value = configuration.getInt(key);
        if (value < 0) {
            throw new IllegalArgumentException("connector option '" + key + "' must not be negative");
        }
        return value;
    }

    private static String requireText(String value, String key) {
        Objects.requireNonNull(value, "connector option '" + key + "' must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException("connector option '" + key + "' must not be blank");
        }
        return value;
    }
}
