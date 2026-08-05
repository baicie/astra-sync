package io.astrasync.connector.api.data;

import java.util.List;
import java.util.Objects;

/** One ordered Debezium delivery batch acknowledged as a single source checkpoint unit. */
public record CdcBatch(long sequence, List<DataEvent> events, CdcPhase phase, boolean snapshotCompleted) {
    public CdcBatch {
        if (sequence <= 0) {
            throw new IllegalArgumentException("sequence must be positive");
        }
        Objects.requireNonNull(events, "events must not be null");
        events = events.stream()
                .map(event -> Objects.requireNonNull(event, "events must not contain null"))
                .toList();
        if (events.isEmpty()) {
            throw new IllegalArgumentException("CDC batch must contain at least one data event");
        }
        phase = Objects.requireNonNull(phase, "phase must not be null");
    }

    public int size() {
        return events.size();
    }
}
