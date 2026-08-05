package io.astrasync.connector.api;

import java.util.Map;

public interface TraceContext {

    String getTraceId();

    String getSpanId();

    Map<String, String> getBaggage();

    TraceContext withBaggage(String key, String value);

    TraceContext withTraceId(String traceId);

    static TraceContext root() {
        return ImmutableTraceContext.rootContext();
    }
}
