package io.astrasync.engine.plan;

import io.astrasync.connector.api.ConnectorRole;
import java.util.Collections;
import java.util.Map;
import java.util.Objects;
import java.util.TreeMap;

public record ConnectorPlan(ConnectorRole role, String connector, String version, Map<String, String> options) {
    public ConnectorPlan {
        role = Objects.requireNonNull(role, "role must not be null");
        connector = requireText(connector, "connector");
        version = requireText(version, "version");
        TreeMap<String, String> ordered = new TreeMap<>();
        Objects.requireNonNull(options, "options must not be null")
                .forEach((key, value) -> ordered.put(
                        requireText(key, "option key"),
                        Objects.requireNonNull(value, "option value must not be null")));
        options = Collections.unmodifiableMap(ordered);
    }

    @Override
    public String toString() {
        return "ConnectorPlan[role=" + role + ", connector=" + connector + ", version=" + version + ", optionKeys="
                + options.keySet() + ']';
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
