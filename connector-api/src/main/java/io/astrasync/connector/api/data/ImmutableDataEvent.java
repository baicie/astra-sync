package io.astrasync.connector.api.data;

import io.astrasync.connector.api.RecordKey;
import io.astrasync.connector.api.SourcePosition;
import io.astrasync.connector.api.TraceContext;
import java.util.Collections;
import java.util.Map;
import java.util.Objects;
import java.util.TreeMap;

/** Immutable connector-neutral change event. */
public record ImmutableDataEvent(
        String eventId,
        SourcePosition sourcePosition,
        Operation operation,
        long eventTime,
        long ingestTime,
        String schemaId,
        String tableId,
        RecordKey key,
        Row before,
        Row after,
        Map<String, String> headers,
        TraceContext traceContext)
        implements DataEvent {
    public ImmutableDataEvent {
        eventId = requireText(eventId, "eventId");
        sourcePosition = Objects.requireNonNull(sourcePosition, "sourcePosition must not be null");
        operation = Objects.requireNonNull(operation, "operation must not be null");
        if (eventTime < 0 || ingestTime < 0) {
            throw new IllegalArgumentException("eventTime and ingestTime must not be negative");
        }
        schemaId = requireText(schemaId, "schemaId");
        tableId = requireText(tableId, "tableId");
        key = Objects.requireNonNull(key, "key must not be null");
        before = Objects.requireNonNull(before, "before must not be null");
        after = Objects.requireNonNull(after, "after must not be null");
        Objects.requireNonNull(headers, "headers must not be null");
        TreeMap<String, String> orderedHeaders = new TreeMap<>();
        headers.forEach((name, value) -> orderedHeaders.put(
                requireText(name, "header name"), Objects.requireNonNull(value, "header value must not be null")));
        headers = Collections.unmodifiableMap(orderedHeaders);
        traceContext = Objects.requireNonNull(traceContext, "traceContext must not be null");
    }

    @Override
    public String getEventId() {
        return eventId;
    }

    @Override
    public SourcePosition getSourcePosition() {
        return sourcePosition;
    }

    @Override
    public Operation getOperation() {
        return operation;
    }

    @Override
    public long getEventTime() {
        return eventTime;
    }

    @Override
    public long getIngestTime() {
        return ingestTime;
    }

    @Override
    public String getSchemaId() {
        return schemaId;
    }

    @Override
    public String getTableId() {
        return tableId;
    }

    @Override
    public RecordKey getKey() {
        return key;
    }

    @Override
    public Row getBefore() {
        return before;
    }

    @Override
    public Row getAfter() {
        return after;
    }

    @Override
    public Map<String, String> getHeaders() {
        return headers;
    }

    @Override
    public TraceContext getTraceContext() {
        return traceContext;
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
