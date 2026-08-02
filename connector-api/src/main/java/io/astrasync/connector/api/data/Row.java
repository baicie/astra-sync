package io.astrasync.connector.api.data;

import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Objects;

/** An immutable, insertion-ordered row of named values. */
public final class Row {
    private static final Row EMPTY = new Row(Map.of());

    private final Map<String, Object> values;

    private Row(Map<String, ?> values) {
        LinkedHashMap<String, Object> copy = new LinkedHashMap<>();
        values.forEach((name, value) -> copy.put(requireColumnName(name), value));
        this.values = Collections.unmodifiableMap(copy);
    }

    public static Row empty() {
        return EMPTY;
    }

    public static Row of(Map<String, ?> values) {
        Objects.requireNonNull(values, "values must not be null");
        return values.isEmpty() ? EMPTY : new Row(values);
    }

    public static Row of(String columnName, Object value) {
        LinkedHashMap<String, Object> values = new LinkedHashMap<>();
        values.put(requireColumnName(columnName), value);
        return new Row(values);
    }

    public Object get(String columnName) {
        return values.get(Objects.requireNonNull(columnName, "columnName must not be null"));
    }

    public boolean contains(String columnName) {
        return values.containsKey(Objects.requireNonNull(columnName, "columnName must not be null"));
    }

    public int size() {
        return values.size();
    }

    public Row with(String columnName, Object value) {
        LinkedHashMap<String, Object> updated = new LinkedHashMap<>(values);
        updated.put(requireColumnName(columnName), value);
        return new Row(updated);
    }

    public Map<String, Object> asMap() {
        return values;
    }

    @Override
    public boolean equals(Object other) {
        return this == other || other instanceof Row row && values.equals(row.values);
    }

    @Override
    public int hashCode() {
        return values.hashCode();
    }

    @Override
    public String toString() {
        return "Row" + values;
    }

    private static String requireColumnName(String columnName) {
        Objects.requireNonNull(columnName, "columnName must not be null");
        if (columnName.isBlank()) {
            throw new IllegalArgumentException("columnName must not be blank");
        }
        return columnName;
    }
}
