package io.astrasync.connector.api;

import java.io.ByteArrayOutputStream;
import java.io.DataOutputStream;
import java.io.IOException;
import java.nio.charset.StandardCharsets;
import java.util.Collections;
import java.util.Map;
import java.util.Objects;
import java.util.TreeMap;

/** Immutable structured record key with deterministic binary encoding. */
public final class ImmutableRecordKey implements RecordKey {
    private static final ImmutableRecordKey EMPTY = new ImmutableRecordKey(Map.of());

    private final Map<String, Object> values;
    private final byte[] bytes;

    private ImmutableRecordKey(Map<String, ?> values) {
        TreeMap<String, Object> ordered = new TreeMap<>();
        values.forEach((key, value) -> ordered.put(requireKey(key), value));
        this.values = Collections.unmodifiableMap(ordered);
        this.bytes = encode(ordered);
    }

    public static ImmutableRecordKey empty() {
        return EMPTY;
    }

    public static ImmutableRecordKey of(Map<String, ?> values) {
        Objects.requireNonNull(values, "values must not be null");
        return values.isEmpty() ? EMPTY : new ImmutableRecordKey(values);
    }

    @Override
    public byte[] toBytes() {
        return bytes.clone();
    }

    @Override
    public Map<String, Object> values() {
        return values;
    }

    @Override
    public boolean equals(Object other) {
        return this == other || other instanceof ImmutableRecordKey key && values.equals(key.values);
    }

    @Override
    public int hashCode() {
        return values.hashCode();
    }

    @Override
    public String toString() {
        return "RecordKey" + values;
    }

    private static byte[] encode(Map<String, Object> values) {
        try {
            ByteArrayOutputStream bytes = new ByteArrayOutputStream();
            try (DataOutputStream output = new DataOutputStream(bytes)) {
                output.writeInt(values.size());
                for (Map.Entry<String, Object> entry : values.entrySet()) {
                    writeUtf8(output, entry.getKey());
                    writeUtf8(output, Objects.toString(entry.getValue(), "null"));
                }
            }
            return bytes.toByteArray();
        } catch (IOException exception) {
            throw new IllegalStateException("failed to encode record key", exception);
        }
    }

    private static void writeUtf8(DataOutputStream output, String value) throws IOException {
        byte[] encoded = value.getBytes(StandardCharsets.UTF_8);
        output.writeInt(encoded.length);
        output.write(encoded);
    }

    private static String requireKey(String key) {
        Objects.requireNonNull(key, "record key name must not be null");
        if (key.isBlank()) {
            throw new IllegalArgumentException("record key name must not be blank");
        }
        return key;
    }
}
