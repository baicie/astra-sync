package io.astrasync.connector.api.source;

import java.util.Objects;

/** Immutable full-load split with an inclusive start and exclusive end boundary. */
public record SourceSplit(String splitId, String sourceId, SplitPosition start, SplitPosition end) {
    public SourceSplit {
        splitId = requireText(splitId, "splitId");
        sourceId = requireText(sourceId, "sourceId");
        start = Objects.requireNonNull(start, "start must not be null");
        end = Objects.requireNonNull(end, "end must not be null");
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
