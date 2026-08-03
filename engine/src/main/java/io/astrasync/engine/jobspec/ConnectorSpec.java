package io.astrasync.engine.jobspec;

import java.util.Collections;
import java.util.Map;
import java.util.Objects;
import java.util.TreeMap;
import java.util.regex.Pattern;

public record ConnectorSpec(String connector, Map<String, String> options) {
    private static final Pattern CONNECTOR_PATTERN = Pattern.compile("(?=.{1,128}$)[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?");

    public ConnectorSpec {
        connector = Objects.requireNonNull(connector, "connector must not be null");
        if (!CONNECTOR_PATTERN.matcher(connector).matches()) {
            throw new IllegalArgumentException("connector has an invalid canonical name");
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
        return "ConnectorSpec[connector=" + connector + ", optionKeys=" + options.keySet() + ']';
    }
}
