package io.astrasync.engine.jobspec;

import java.util.Collections;
import java.util.Map;
import java.util.Objects;
import java.util.TreeMap;

public record TransformSpec(String type, Map<String, String> options) {
    public TransformSpec {
        type = Objects.requireNonNull(type, "type must not be null");
        if (type.isBlank()) {
            throw new IllegalArgumentException("transform type must not be blank");
        }
        TreeMap<String, String> ordered = new TreeMap<>();
        Objects.requireNonNull(options, "options must not be null").forEach((key, value) -> {
            if (key == null || key.isBlank()) {
                throw new IllegalArgumentException("option keys must not be blank");
            }
            ordered.put(key, Objects.requireNonNull(value, "option values must not be null"));
        });
        options = Collections.unmodifiableMap(ordered);
    }

    @Override
    public String toString() {
        return "TransformSpec[type=" + type + ", optionKeys=" + options.keySet() + ']';
    }
}
