package io.astrasync.connector.api.data;

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
