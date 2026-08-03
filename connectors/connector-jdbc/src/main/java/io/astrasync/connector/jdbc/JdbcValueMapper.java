package io.astrasync.connector.jdbc;

import io.astrasync.connector.api.data.Row;
import java.math.BigDecimal;
import java.sql.ResultSet;
import java.sql.ResultSetMetaData;
import java.sql.SQLException;
import java.sql.Types;
import java.time.LocalDate;
import java.time.LocalDateTime;
import java.time.LocalTime;
import java.time.OffsetDateTime;
import java.util.Arrays;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Set;

final class JdbcValueMapper {
    private JdbcValueMapper() {}

    static List<Column> columns(ResultSetMetaData metadata) throws SQLException {
        int count = metadata.getColumnCount();
        if (count == 0) {
            throw new IllegalArgumentException("JDBC query returned no columns");
        }
        Set<String> names = new HashSet<>();
        Column[] columns = new Column[count];
        for (int index = 1; index <= count; index++) {
            String label = metadata.getColumnLabel(index);
            if (label == null || label.isBlank()) {
                throw new IllegalArgumentException("JDBC query column " + index + " has a blank label");
            }
            if (!names.add(label)) {
                throw new IllegalArgumentException("JDBC query has duplicate column label '" + label + "'");
            }
            columns[index - 1] = new Column(label, metadata.getColumnType(index), metadata.getColumnTypeName(index));
        }
        return List.of(columns);
    }

    static Row readRow(ResultSet resultSet, List<Column> columns) throws SQLException {
        LinkedHashMap<String, Object> values = new LinkedHashMap<>();
        for (int index = 0; index < columns.size(); index++) {
            Column column = columns.get(index);
            values.put(column.label(), readValue(resultSet, index + 1, column));
        }
        return Row.of(values);
    }

    static void validateSinkValue(String column, Object value) {
        if (value == null
                || value instanceof String
                || value instanceof Boolean
                || value instanceof Byte
                || value instanceof Short
                || value instanceof Integer
                || value instanceof Long
                || value instanceof Float
                || value instanceof Double
                || value instanceof BigDecimal
                || value instanceof LocalDate
                || value instanceof LocalTime
                || value instanceof LocalDateTime
                || value instanceof OffsetDateTime
                || value instanceof byte[]) {
            return;
        }
        throw new IllegalArgumentException("JDBC sink column '" + column + "' does not support Java value type "
                + value.getClass().getName());
    }

    private static Object readValue(ResultSet resultSet, int index, Column column) throws SQLException {
        return switch (column.sqlType()) {
            case Types.NULL -> null;
            case Types.CHAR,
                    Types.VARCHAR,
                    Types.LONGVARCHAR,
                    Types.NCHAR,
                    Types.NVARCHAR,
                    Types.LONGNVARCHAR,
                    Types.CLOB,
                    Types.SQLXML -> resultSet.getString(index);
            case Types.BOOLEAN, Types.BIT -> nullableBoolean(resultSet, index);
            case Types.TINYINT -> nullableByte(resultSet, index);
            case Types.SMALLINT -> nullableShort(resultSet, index);
            case Types.INTEGER -> nullableInt(resultSet, index);
            case Types.BIGINT -> nullableLong(resultSet, index);
            case Types.REAL -> nullableFloat(resultSet, index);
            case Types.FLOAT, Types.DOUBLE -> nullableDouble(resultSet, index);
            case Types.DECIMAL, Types.NUMERIC -> resultSet.getBigDecimal(index);
            case Types.DATE -> localDate(resultSet.getObject(index));
            case Types.TIME, Types.TIME_WITH_TIMEZONE -> localTime(resultSet.getObject(index));
            case Types.TIMESTAMP -> localDateTime(resultSet.getObject(index));
            case Types.TIMESTAMP_WITH_TIMEZONE -> offsetDateTime(resultSet.getObject(index));
            case Types.BINARY, Types.VARBINARY, Types.LONGVARBINARY, Types.BLOB -> copyBytes(resultSet.getBytes(index));
            default -> throw unsupported(column);
        };
    }

    private static Boolean nullableBoolean(ResultSet resultSet, int index) throws SQLException {
        boolean value = resultSet.getBoolean(index);
        return resultSet.wasNull() ? null : value;
    }

    private static Byte nullableByte(ResultSet resultSet, int index) throws SQLException {
        byte value = resultSet.getByte(index);
        return resultSet.wasNull() ? null : value;
    }

    private static Short nullableShort(ResultSet resultSet, int index) throws SQLException {
        short value = resultSet.getShort(index);
        return resultSet.wasNull() ? null : value;
    }

    private static Integer nullableInt(ResultSet resultSet, int index) throws SQLException {
        int value = resultSet.getInt(index);
        return resultSet.wasNull() ? null : value;
    }

    private static Long nullableLong(ResultSet resultSet, int index) throws SQLException {
        long value = resultSet.getLong(index);
        return resultSet.wasNull() ? null : value;
    }

    private static Float nullableFloat(ResultSet resultSet, int index) throws SQLException {
        float value = resultSet.getFloat(index);
        return resultSet.wasNull() ? null : value;
    }

    private static Double nullableDouble(ResultSet resultSet, int index) throws SQLException {
        double value = resultSet.getDouble(index);
        return resultSet.wasNull() ? null : value;
    }

    private static LocalDate localDate(Object value) {
        if (value == null) {
            return null;
        }
        if (value instanceof LocalDate localDate) {
            return localDate;
        }
        if (value instanceof java.sql.Date sqlDate) {
            return sqlDate.toLocalDate();
        }
        throw new IllegalArgumentException(
                "JDBC DATE value has unsupported Java type " + value.getClass().getName());
    }

    private static LocalTime localTime(Object value) {
        if (value == null) {
            return null;
        }
        if (value instanceof LocalTime localTime) {
            return localTime;
        }
        if (value instanceof java.sql.Time sqlTime) {
            return sqlTime.toLocalTime();
        }
        throw new IllegalArgumentException(
                "JDBC TIME value has unsupported Java type " + value.getClass().getName());
    }

    private static LocalDateTime localDateTime(Object value) {
        if (value == null) {
            return null;
        }
        if (value instanceof LocalDateTime localDateTime) {
            return localDateTime;
        }
        if (value instanceof java.sql.Timestamp timestamp) {
            return timestamp.toLocalDateTime();
        }
        throw new IllegalArgumentException("JDBC TIMESTAMP value has unsupported Java type "
                + value.getClass().getName());
    }

    private static OffsetDateTime offsetDateTime(Object value) {
        if (value == null) {
            return null;
        }
        if (value instanceof OffsetDateTime offsetDateTime) {
            return offsetDateTime;
        }
        throw new IllegalArgumentException("JDBC TIMESTAMP_WITH_TIMEZONE value has unsupported Java type "
                + value.getClass().getName());
    }

    private static byte[] copyBytes(byte[] value) {
        return value == null ? null : Arrays.copyOf(value, value.length);
    }

    private static IllegalArgumentException unsupported(Column column) {
        return new IllegalArgumentException(
                "unsupported JDBC type '" + column.typeName() + "' for column '" + column.label() + "'");
    }

    record Column(String label, int sqlType, String typeName) {}
}
