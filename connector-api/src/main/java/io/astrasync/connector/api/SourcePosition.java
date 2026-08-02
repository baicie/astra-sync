package io.astrasync.connector.api;

import java.util.Map;
import java.util.Optional;

public interface SourcePosition {

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
