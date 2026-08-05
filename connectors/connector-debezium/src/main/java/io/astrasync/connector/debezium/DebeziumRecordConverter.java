package io.astrasync.connector.debezium;

import io.astrasync.connector.api.RecordKey;
import io.astrasync.connector.api.SourcePosition;
import io.astrasync.connector.api.TraceContext;
import io.astrasync.connector.api.data.DataEvent;
import io.astrasync.connector.api.data.ImmutableDataEvent;
import io.astrasync.connector.api.data.Row;
import java.lang.reflect.Array;
import java.nio.ByteBuffer;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.time.Clock;
import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.Optional;
import java.util.TreeMap;
import java.util.concurrent.atomic.AtomicLong;
import org.apache.kafka.connect.data.Field;
import org.apache.kafka.connect.data.Struct;
import org.apache.kafka.connect.source.SourceRecord;

/** Converts Debezium Connect records into connector-neutral AstraSync events. */
public final class DebeziumRecordConverter {
    private final Clock clock;
    private final AtomicLong fallbackEventIndex = new AtomicLong();

    public DebeziumRecordConverter() {
        this(Clock.systemUTC());
    }

    DebeziumRecordConverter(Clock clock) {
        this.clock = Objects.requireNonNull(clock, "clock must not be null");
    }

    public Optional<DataEvent> convert(SourceRecord record) {
        Objects.requireNonNull(record, "record must not be null");
        if (!(record.value() instanceof Struct envelope)) {
            return Optional.empty();
        }
        String operationCode = text(field(envelope, "op"));
        boolean schemaChange = operationCode.isEmpty() && field(envelope, "ddl") != null;
        if (operationCode.isEmpty() && !schemaChange) {
            return Optional.empty();
        }

        Struct source = field(envelope, "source") instanceof Struct sourceStruct ? sourceStruct : null;
        DataEvent.Operation operation = schemaChange
                ? DataEvent.Operation.SCHEMA_CHANGE
                : operation(operationCode).orElse(null);
        if (operation == null) {
            return Optional.empty();
        }

        String database = firstText(field(source, "db"), field(source, "database"));
        String schema = text(field(source, "schema"));
        String table = text(field(source, "table"));
        String tableId = tableId(record.topic(), database, schema, table);
        Map<String, String> offsets = stringMap(record.sourceOffset());
        String sourceInstance =
                firstText(field(source, "name"), record.sourcePartition().get("server"), record.topic());
        if (sourceInstance.isEmpty()) {
            sourceInstance = "unknown";
        }
        long eventTime = longValue(field(source, "ts_ms"), record.timestamp(), clock.millis());
        long eventIndex = eventIndex(offsets);
        String positionId = digest(
                sourceInstance + '|' + canonical(record.sourcePartition()) + '|' + canonical(record.sourceOffset()));
        String transactionId = transactionId(envelope, source);
        SourcePosition position = SourcePosition.of(
                positionId, sourceInstance, database, tableId, offsets, eventTime, transactionId, eventIndex);

        Map<String, String> headers = headers(source, transactionId);
        Row before = schemaChange ? Row.empty() : row(field(envelope, "before"));
        Row after = schemaChange ? Row.of("ddl", field(envelope, "ddl")) : row(field(envelope, "after"));
        String schemaId = schemaId(record, tableId);
        String eventId = digest(positionId + '|' + operation + '|' + tableId);
        return Optional.of(new ImmutableDataEvent(
                eventId,
                position,
                operation,
                eventTime,
                clock.millis(),
                schemaId,
                tableId,
                recordKey(record.key()),
                before,
                after,
                headers,
                TraceContext.root()));
    }

    private long eventIndex(Map<String, String> offsets) {
        for (String candidate : List.of("event", "row", "lsn", "pos")) {
            String value = offsets.get(candidate);
            if (value != null) {
                try {
                    long parsed = Long.parseUnsignedLong(value);
                    return parsed < 0 ? Long.MAX_VALUE : parsed;
                } catch (NumberFormatException ignored) {
                    // Try the next source-specific offset before using a local sequence.
                }
            }
        }
        return fallbackEventIndex.incrementAndGet();
    }

    private static Optional<DataEvent.Operation> operation(String code) {
        return Optional.ofNullable(
                switch (code) {
                    case "c" -> DataEvent.Operation.INSERT;
                    case "r" -> DataEvent.Operation.SNAPSHOT;
                    case "u" -> DataEvent.Operation.UPDATE;
                    case "d" -> DataEvent.Operation.DELETE;
                    case "t" -> DataEvent.Operation.TRUNCATE;
                    default -> null;
                });
    }

    private static RecordKey recordKey(Object key) {
        if (key instanceof Struct struct) {
            LinkedHashMap<String, Object> values = new LinkedHashMap<>();
            for (Field field : struct.schema().fields()) {
                values.put(field.name(), normalize(struct.get(field)));
            }
            return RecordKey.of(values);
        }
        if (key instanceof Map<?, ?> map) {
            TreeMap<String, Object> values = new TreeMap<>();
            map.forEach((name, value) -> values.put(Objects.toString(name), normalize(value)));
            return RecordKey.of(values);
        }
        return key == null ? RecordKey.empty() : RecordKey.of(Map.of("value", normalize(key)));
    }

    private static Row row(Object value) {
        if (value == null) {
            return Row.empty();
        }
        if (value instanceof Struct struct) {
            LinkedHashMap<String, Object> values = new LinkedHashMap<>();
            for (Field field : struct.schema().fields()) {
                values.put(field.name(), normalize(struct.get(field)));
            }
            return Row.of(values);
        }
        if (value instanceof Map<?, ?> map) {
            LinkedHashMap<String, Object> values = new LinkedHashMap<>();
            map.forEach((name, fieldValue) -> values.put(Objects.toString(name), normalize(fieldValue)));
            return Row.of(values);
        }
        return Row.of("value", normalize(value));
    }

    private static Object normalize(Object value) {
        if (value instanceof Struct struct) {
            LinkedHashMap<String, Object> normalized = new LinkedHashMap<>();
            for (Field field : struct.schema().fields()) {
                normalized.put(field.name(), normalize(struct.get(field)));
            }
            return Collections.unmodifiableMap(normalized);
        }
        if (value instanceof Map<?, ?> map) {
            TreeMap<String, Object> normalized = new TreeMap<>();
            map.forEach((key, item) -> normalized.put(Objects.toString(key), normalize(item)));
            return Collections.unmodifiableMap(normalized);
        }
        if (value instanceof Collection<?> collection) {
            return collection.stream().map(DebeziumRecordConverter::normalize).toList();
        }
        if (value instanceof ByteBuffer buffer) {
            ByteBuffer duplicate = buffer.duplicate();
            byte[] bytes = new byte[duplicate.remaining()];
            duplicate.get(bytes);
            return bytes;
        }
        if (value != null && value.getClass().isArray() && !(value instanceof byte[])) {
            List<Object> normalized = new ArrayList<>(Array.getLength(value));
            for (int index = 0; index < Array.getLength(value); index++) {
                normalized.add(normalize(Array.get(value, index)));
            }
            return List.copyOf(normalized);
        }
        return value;
    }

    private static Map<String, String> headers(Struct source, String transactionId) {
        TreeMap<String, String> headers = new TreeMap<>();
        putIfText(headers, "source.connector", field(source, "connector"));
        putIfText(headers, "source.version", field(source, "version"));
        putIfText(headers, "source.snapshot", field(source, "snapshot"));
        putIfText(headers, "source.name", field(source, "name"));
        if (!transactionId.isEmpty()) {
            headers.put("source.transaction.id", transactionId);
        }
        return headers;
    }

    private static String transactionId(Struct envelope, Struct source) {
        Object transaction = field(envelope, "transaction");
        if (transaction instanceof Struct transactionStruct) {
            String id = text(field(transactionStruct, "id"));
            if (!id.isEmpty()) {
                return id;
            }
        }
        return firstText(field(source, "txId"), field(source, "transaction_id"));
    }

    private static String schemaId(SourceRecord record, String tableId) {
        if (record.valueSchema() == null) {
            return tableId;
        }
        String name = record.valueSchema().name();
        String base = name == null || name.isBlank() ? tableId : name;
        Integer version = record.valueSchema().version();
        return version == null ? base : base + ':' + version;
    }

    private static String tableId(String topic, String database, String schema, String table) {
        if (!schema.isEmpty() && !table.isEmpty()) {
            return schema + '.' + table;
        }
        if (!database.isEmpty() && !table.isEmpty()) {
            return database + '.' + table;
        }
        return topic == null || topic.isBlank() ? "unknown" : topic;
    }

    private static Map<String, String> stringMap(Map<String, ?> source) {
        TreeMap<String, String> result = new TreeMap<>();
        if (source != null) {
            source.forEach((key, value) -> result.put(key, canonical(value)));
        }
        return result;
    }

    private static String canonical(Object value) {
        if (value instanceof Map<?, ?> map) {
            TreeMap<String, String> ordered = new TreeMap<>();
            map.forEach((key, item) -> ordered.put(Objects.toString(key), canonical(item)));
            return ordered.toString();
        }
        if (value instanceof Collection<?> collection) {
            return collection.stream()
                    .map(DebeziumRecordConverter::canonical)
                    .toList()
                    .toString();
        }
        return Objects.toString(value, "null");
    }

    private static Object field(Struct struct, String name) {
        if (struct == null || struct.schema().field(name) == null) {
            return null;
        }
        return struct.get(name);
    }

    private static String firstText(Object... values) {
        for (Object value : values) {
            String candidate = text(value);
            if (!candidate.isEmpty()) {
                return candidate;
            }
        }
        return "";
    }

    private static String text(Object value) {
        return value == null ? "" : value.toString();
    }

    private static long longValue(Object first, Object second, long fallback) {
        for (Object value : new Object[] {first, second, fallback}) {
            if (value instanceof Number number) {
                return Math.max(0, number.longValue());
            }
            if (value != null) {
                try {
                    return Math.max(0, Long.parseLong(value.toString()));
                } catch (NumberFormatException ignored) {
                    // Continue to the next timestamp representation.
                }
            }
        }
        return fallback;
    }

    private static void putIfText(Map<String, String> target, String key, Object value) {
        String text = text(value);
        if (!text.isEmpty()) {
            target.put(key, text);
        }
    }

    private static String digest(String value) {
        try {
            byte[] digest = MessageDigest.getInstance("SHA-256")
                    .digest(value.getBytes(java.nio.charset.StandardCharsets.UTF_8));
            StringBuilder result = new StringBuilder(digest.length * 2);
            for (byte item : digest) {
                result.append(Character.forDigit((item >>> 4) & 0xf, 16));
                result.append(Character.forDigit(item & 0xf, 16));
            }
            return result.toString();
        } catch (NoSuchAlgorithmException exception) {
            throw new IllegalStateException("SHA-256 is not available", exception);
        }
    }
}
