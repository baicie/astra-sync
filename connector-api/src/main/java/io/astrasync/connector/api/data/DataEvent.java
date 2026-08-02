package io.astrasync.connector.api.data;

import io.astrasync.connector.api.RecordKey;
import io.astrasync.connector.api.SourcePosition;
import io.astrasync.connector.api.TraceContext;
import java.util.Map;

public interface DataEvent {

    String getEventId();

    SourcePosition getSourcePosition();

    Operation getOperation();

    long getEventTime();

    long getIngestTime();

    String getSchemaId();

    String getTableId();

    RecordKey getKey();

    Record getBefore();

    Record getAfter();

    Map<String, String> getHeaders();

    TraceContext getTraceContext();

    enum Operation {
        INSERT,
        UPDATE,
        DELETE,
        SNAPSHOT,
        TRUNCATE,
        SCHEMA_CHANGE
    }
}
