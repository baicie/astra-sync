package io.astrasync.connector.debezium;

import io.astrasync.connector.api.source.SplitPosition;
import java.nio.ByteBuffer;
import java.util.ArrayList;
import java.util.Base64;
import java.util.Comparator;
import java.util.HashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.TreeMap;

final class OffsetStateCodec {
    private static final String FORMAT_KEY = "astrasync.cdc.offset.format";
    private static final String CONNECTOR_KEY = "astrasync.cdc.offset.connector";
    private static final String COUNT_KEY = "astrasync.cdc.offset.count";
    private static final String FORMAT_VERSION = "1";

    private OffsetStateCodec() {}

    static SplitPosition encode(String connectorIdentity, Map<ByteBuffer, ByteBuffer> offsets) {
        Objects.requireNonNull(offsets, "offsets must not be null");
        List<Entry> entries = offsets.entrySet().stream()
                .filter(entry -> entry.getValue() != null)
                .map(entry -> new Entry(bytes(entry.getKey()), bytes(entry.getValue())))
                .sorted(Comparator.comparing(entry -> Base64.getEncoder().encodeToString(entry.key())))
                .toList();
        TreeMap<String, String> encoded = new TreeMap<>();
        encoded.put(FORMAT_KEY, FORMAT_VERSION);
        encoded.put(CONNECTOR_KEY, requireText(connectorIdentity, "connectorIdentity"));
        encoded.put(COUNT_KEY, Integer.toString(entries.size()));
        for (int index = 0; index < entries.size(); index++) {
            String prefix = "astrasync.cdc.offset.entry." + String.format("%04d", index) + ".";
            encoded.put(
                    prefix + "key",
                    Base64.getEncoder().encodeToString(entries.get(index).key()));
            encoded.put(
                    prefix + "value",
                    Base64.getEncoder().encodeToString(entries.get(index).value()));
        }
        return new SplitPosition(encoded);
    }

    static Map<ByteBuffer, ByteBuffer> decode(String connectorIdentity, SplitPosition position) {
        Objects.requireNonNull(position, "position must not be null");
        if (position.isUnbounded()) {
            return Map.of();
        }
        Map<String, String> offsets = position.offsets();
        if (!FORMAT_VERSION.equals(offsets.get(FORMAT_KEY))) {
            throw new IllegalArgumentException("unsupported or non-CDC resume position");
        }
        if (!requireText(connectorIdentity, "connectorIdentity").equals(offsets.get(CONNECTOR_KEY))) {
            throw new IllegalArgumentException("CDC resume position belongs to a different connector");
        }
        int count;
        try {
            count = Integer.parseInt(Objects.requireNonNull(offsets.get(COUNT_KEY), "CDC offset count is missing"));
        } catch (NumberFormatException exception) {
            throw new IllegalArgumentException("CDC offset count is invalid", exception);
        }
        if (count < 0) {
            throw new IllegalArgumentException("CDC offset count must not be negative");
        }
        HashMap<ByteBuffer, ByteBuffer> decoded = new HashMap<>();
        List<String> expectedKeys = new ArrayList<>();
        expectedKeys.add(FORMAT_KEY);
        expectedKeys.add(CONNECTOR_KEY);
        expectedKeys.add(COUNT_KEY);
        for (int index = 0; index < count; index++) {
            String prefix = "astrasync.cdc.offset.entry." + String.format("%04d", index) + ".";
            String keyName = prefix + "key";
            String valueName = prefix + "value";
            expectedKeys.add(keyName);
            expectedKeys.add(valueName);
            try {
                decoded.put(
                        ByteBuffer.wrap(Base64.getDecoder().decode(requiredOffset(offsets, keyName))),
                        ByteBuffer.wrap(Base64.getDecoder().decode(requiredOffset(offsets, valueName))));
            } catch (IllegalArgumentException exception) {
                throw new IllegalArgumentException("CDC offset entry " + index + " is not valid Base64", exception);
            }
        }
        if (!offsets.keySet().equals(java.util.Set.copyOf(expectedKeys))) {
            throw new IllegalArgumentException("CDC resume position contains unexpected fields");
        }
        return decoded;
    }

    static Map<ByteBuffer, ByteBuffer> copy(Map<ByteBuffer, ByteBuffer> source) {
        HashMap<ByteBuffer, ByteBuffer> copy = new HashMap<>();
        source.forEach((key, value) ->
                copy.put(ByteBuffer.wrap(bytes(key)), value == null ? null : ByteBuffer.wrap(bytes(value))));
        return copy;
    }

    private static byte[] bytes(ByteBuffer buffer) {
        ByteBuffer duplicate =
                Objects.requireNonNull(buffer, "offset buffer must not be null").duplicate();
        byte[] value = new byte[duplicate.remaining()];
        duplicate.get(value);
        return value;
    }

    private static String requiredOffset(Map<String, String> offsets, String key) {
        return Objects.requireNonNull(offsets.get(key), "CDC offset field is missing: " + key);
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }

    private record Entry(byte[] key, byte[] value) {}
}
