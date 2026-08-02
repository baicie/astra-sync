package io.astrasync.connector.api;

public interface TraceContext {

    String getTraceId();

    String getSpanId();

    Map<String, String> getBaggage();

    TraceContext withBaggage(String key, String value);

    TraceContext withTraceId(String traceId);

    static TraceContext root() {
        throw new UnsupportedOperationException("Implement in subclass");
    }
}
