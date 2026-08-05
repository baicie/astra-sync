package io.astrasync.connector.api;

import java.util.Map;
import java.util.Optional;

public interface SourcePosition {

    static SourcePosition of(
            String positionId,
            String sourceInstance,
            String database,
            String table,
            Map<String, String> offset,
            long timestamp,
            String transactionId,
            long eventIndex) {
        return new ImmutableSourcePosition(
                positionId, sourceInstance, database, table, offset, timestamp, transactionId, eventIndex);
    }

    String getPositionId();

    String getSourceInstance();

    String getDatabase();

    String getTable();

    Map<String, String> getOffset();

    long getTimestamp();

    String getTransactionId();

    long getEventIndex();

    Optional<SourcePosition> earlierThan(SourcePosition other);

    Optional<SourcePosition> laterThan(SourcePosition other);

    boolean isBefore(SourcePosition other);
}
