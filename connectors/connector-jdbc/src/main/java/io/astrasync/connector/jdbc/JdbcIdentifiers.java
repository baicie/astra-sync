package io.astrasync.connector.jdbc;

import java.sql.DatabaseMetaData;
import java.sql.SQLException;
import java.util.List;

final class JdbcIdentifiers {
    private JdbcIdentifiers() {}

    static String quoteTable(DatabaseMetaData metadata, String table) throws SQLException {
        String quote = quoteString(metadata);
        return join(table.split("\\.", -1), quote);
    }

    static String quoteColumns(DatabaseMetaData metadata, List<String> columns) throws SQLException {
        String quote = quoteString(metadata);
        return columns.stream()
                .map(column -> quote(column, quote))
                .reduce((left, right) -> left + ", " + right)
                .orElseThrow();
    }

    private static String join(String[] identifiers, String quote) {
        return String.join(
                ".",
                java.util.Arrays.stream(identifiers)
                        .map(identifier -> quote(identifier, quote))
                        .toList());
    }

    private static String quoteString(DatabaseMetaData metadata) throws SQLException {
        String value = metadata.getIdentifierQuoteString();
        if (value == null || value.isBlank()) {
            return "";
        }
        return value.substring(0, 1);
    }

    private static String quote(String identifier, String quote) {
        if (quote.isEmpty()) {
            return identifier;
        }
        return quote + identifier.replace(quote, quote + quote) + quote;
    }
}
