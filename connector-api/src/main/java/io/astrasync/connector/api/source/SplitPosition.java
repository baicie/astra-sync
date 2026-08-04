package io.astrasync.connector.api.source;

import com.fasterxml.jackson.annotation.JsonIgnore;
import java.util.Collections;
import java.util.Map;
import java.util.Objects;
import java.util.TreeMap;

/** An opaque, stable position map used as a split boundary. An empty map means unbounded. */
public record SplitPosition(Map<String, String> offsets) {
    public SplitPosition {
        Objects.requireNonNull(offsets, "offsets must not be null");
        TreeMap<String, String> ordered = new TreeMap<>();
        offsets.forEach((key, value) -> {
            if (key == null || key.isBlank()) {
                throw new IllegalArgumentException("position key must not be blank");
            }
            ordered.put(key, Objects.requireNonNull(value, "position value must not be null"));
        });
        offsets = Collections.unmodifiableMap(ordered);
    }

    public static SplitPosition unbounded() {
        return new SplitPosition(Map.of());
    }

    @JsonIgnore
    public boolean isUnbounded() {
        return offsets.isEmpty();
    }
}
