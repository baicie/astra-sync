package io.astrasync.engine.kernel;

import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.Map;
import java.util.Objects;

public final class SyncRecord {
    private final Map<String, Object> values;

    private SyncRecord(Map<String, Object> values) {
        LinkedHashMap<String, Object> copy = new LinkedHashMap<>();
        values.forEach((key, value) -> copy.put(Objects.requireNonNull(key, "record key must not be null"), value));
        this.values = Collections.unmodifiableMap(copy);
    }

    public static SyncRecord of(Map<String, Object> values) {
        return new SyncRecord(Objects.requireNonNull(values, "values must not be null"));
    }

    public static SyncRecord of(String key, Object value) {
        Map<String, Object> values = new LinkedHashMap<>();
        values.put(key, value);
        return of(values);
    }

    public Object get(String key) {
        return values.get(key);
    }

    public SyncRecord with(String key, Object value) {
        Map<String, Object> updated = new LinkedHashMap<>(values);
        updated.put(Objects.requireNonNull(key, "key must not be null"), value);
        return new SyncRecord(updated);
    }

    public Map<String, Object> asMap() {
        return values;
    }

    @Override
    public boolean equals(Object other) {
        return this == other || other instanceof SyncRecord record && values.equals(record.values);
    }

    @Override
    public int hashCode() {
        return values.hashCode();
    }

    @Override
    public String toString() {
        return "SyncRecord" + values;
    }
}
