package io.astrasync.connector.api;

import java.util.Collections;
import java.util.Map;
import java.util.Objects;
import java.util.Optional;
import java.util.TreeMap;

/** Immutable logical database-log position attached to a change event. */
public final class ImmutableSourcePosition implements SourcePosition {
    private final String positionId;
    private final String sourceInstance;
    private final String database;
    private final String table;
    private final Map<String, String> offset;
    private final long timestamp;
    private final String transactionId;
    private final long eventIndex;

    public ImmutableSourcePosition(
            String positionId,
            String sourceInstance,
            String database,
            String table,
            Map<String, String> offset,
            long timestamp,
            String transactionId,
            long eventIndex) {
        this.positionId = requireText(positionId, "positionId");
        this.sourceInstance = requireText(sourceInstance, "sourceInstance");
        this.database = Objects.requireNonNull(database, "database must not be null");
        this.table = Objects.requireNonNull(table, "table must not be null");
        Objects.requireNonNull(offset, "offset must not be null");
        TreeMap<String, String> ordered = new TreeMap<>();
        offset.forEach((key, value) -> ordered.put(
                requireText(key, "offset key"), Objects.requireNonNull(value, "offset value must not be null")));
        this.offset = Collections.unmodifiableMap(ordered);
        if (timestamp < 0 || eventIndex < 0) {
            throw new IllegalArgumentException("timestamp and eventIndex must not be negative");
        }
        this.timestamp = timestamp;
        this.transactionId = Objects.requireNonNull(transactionId, "transactionId must not be null");
        this.eventIndex = eventIndex;
    }

    @Override
    public String getPositionId() {
        return positionId;
    }

    @Override
    public String getSourceInstance() {
        return sourceInstance;
    }

    @Override
    public String getDatabase() {
        return database;
    }

    @Override
    public String getTable() {
        return table;
    }

    @Override
    public Map<String, String> getOffset() {
        return offset;
    }

    @Override
    public long getTimestamp() {
        return timestamp;
    }

    @Override
    public String getTransactionId() {
        return transactionId;
    }

    @Override
    public long getEventIndex() {
        return eventIndex;
    }

    @Override
    public Optional<SourcePosition> earlierThan(SourcePosition other) {
        return compare(other).map(value -> value <= 0 ? this : other);
    }

    @Override
    public Optional<SourcePosition> laterThan(SourcePosition other) {
        return compare(other).map(value -> value >= 0 ? this : other);
    }

    @Override
    public boolean isBefore(SourcePosition other) {
        return compare(other).map(value -> value < 0).orElse(false);
    }

    @Override
    public boolean equals(Object other) {
        return this == other || other instanceof SourcePosition position && positionId.equals(position.getPositionId());
    }

    @Override
    public int hashCode() {
        return positionId.hashCode();
    }

    @Override
    public String toString() {
        return "SourcePosition[id=" + positionId + ", source=" + sourceInstance + ", offset=" + offset + ']';
    }

    private Optional<Integer> compare(SourcePosition other) {
        Objects.requireNonNull(other, "other must not be null");
        if (!sourceInstance.equals(other.getSourceInstance())) {
            return Optional.empty();
        }
        int timestampComparison = Long.compare(timestamp, other.getTimestamp());
        if (timestampComparison != 0) {
            return Optional.of(timestampComparison);
        }
        int eventComparison = Long.compare(eventIndex, other.getEventIndex());
        return Optional.of(eventComparison != 0 ? eventComparison : positionId.compareTo(other.getPositionId()));
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
