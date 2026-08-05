package io.astrasync.connector.api;

import java.util.Collections;
import java.util.Map;
import java.util.Objects;
import java.util.TreeMap;

/** Immutable trace context used when a connector has no upstream tracing carrier. */
public final class ImmutableTraceContext implements TraceContext {
    private static final ImmutableTraceContext ROOT = new ImmutableTraceContext("root", "root", Map.of());

    private final String traceId;
    private final String spanId;
    private final Map<String, String> baggage;

    public ImmutableTraceContext(String traceId, String spanId, Map<String, String> baggage) {
        this.traceId = requireText(traceId, "traceId");
        this.spanId = requireText(spanId, "spanId");
        Objects.requireNonNull(baggage, "baggage must not be null");
        TreeMap<String, String> ordered = new TreeMap<>();
        baggage.forEach((key, value) -> ordered.put(
                requireText(key, "baggage key"), Objects.requireNonNull(value, "baggage value must not be null")));
        this.baggage = Collections.unmodifiableMap(ordered);
    }

    static ImmutableTraceContext rootContext() {
        return ROOT;
    }

    @Override
    public String getTraceId() {
        return traceId;
    }

    @Override
    public String getSpanId() {
        return spanId;
    }

    @Override
    public Map<String, String> getBaggage() {
        return baggage;
    }

    @Override
    public TraceContext withBaggage(String key, String value) {
        TreeMap<String, String> updated = new TreeMap<>(baggage);
        updated.put(requireText(key, "baggage key"), Objects.requireNonNull(value, "value must not be null"));
        return new ImmutableTraceContext(traceId, spanId, updated);
    }

    @Override
    public TraceContext withTraceId(String updatedTraceId) {
        return new ImmutableTraceContext(updatedTraceId, spanId, baggage);
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
