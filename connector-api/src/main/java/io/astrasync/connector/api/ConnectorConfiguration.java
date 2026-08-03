package io.astrasync.connector.api;

import java.util.Collections;
import java.util.Map;
import java.util.Objects;
import java.util.Optional;
import java.util.TreeMap;

/** Immutable connector options with strict typed accessors. */
public final class ConnectorConfiguration {
    private static final ConnectorConfiguration EMPTY = new ConnectorConfiguration(Map.of());

    private final Map<String, String> options;

    private ConnectorConfiguration(Map<String, String> options) {
        TreeMap<String, String> copy = new TreeMap<>();
        options.forEach((key, value) -> copy.put(requireKey(key), Objects.requireNonNull(value, valueMessage(key))));
        this.options = Collections.unmodifiableMap(copy);
    }

    public static ConnectorConfiguration empty() {
        return EMPTY;
    }

    public static ConnectorConfiguration of(Map<String, String> options) {
        Objects.requireNonNull(options, "options must not be null");
        return options.isEmpty() ? EMPTY : new ConnectorConfiguration(options);
    }

    public String required(String key) {
        String normalizedKey = requireKey(key);
        String value = options.get(normalizedKey);
        if (value == null) {
            throw new IllegalArgumentException("missing required connector option '" + normalizedKey + "'");
        }
        return value;
    }

    public Optional<String> optional(String key) {
        return Optional.ofNullable(options.get(requireKey(key)));
    }

    public int getInt(String key) {
        return parseInt(key, required(key));
    }

    public int getInt(String key, int defaultValue) {
        String normalizedKey = requireKey(key);
        String value = options.get(normalizedKey);
        return value == null ? defaultValue : parseInt(normalizedKey, value);
    }

    public boolean getBoolean(String key) {
        return parseBoolean(key, required(key));
    }

    public boolean getBoolean(String key, boolean defaultValue) {
        String normalizedKey = requireKey(key);
        String value = options.get(normalizedKey);
        return value == null ? defaultValue : parseBoolean(normalizedKey, value);
    }

    public boolean contains(String key) {
        return options.containsKey(requireKey(key));
    }

    public Map<String, String> asMap() {
        return options;
    }

    @Override
    public boolean equals(Object other) {
        return this == other
                || other instanceof ConnectorConfiguration configuration && options.equals(configuration.options);
    }

    @Override
    public int hashCode() {
        return options.hashCode();
    }

    @Override
    public String toString() {
        return "ConnectorConfiguration{keys=" + options.keySet() + '}';
    }

    private static int parseInt(String key, String value) {
        try {
            return Integer.parseInt(value);
        } catch (NumberFormatException exception) {
            throw new IllegalArgumentException("connector option '" + key + "' must be an integer", exception);
        }
    }

    private static boolean parseBoolean(String key, String value) {
        return switch (value) {
            case "true" -> true;
            case "false" -> false;
            default -> throw new IllegalArgumentException("connector option '" + key + "' must be 'true' or 'false'");
        };
    }

    private static String requireKey(String key) {
        Objects.requireNonNull(key, "key must not be null");
        if (key.isBlank()) {
            throw new IllegalArgumentException("key must not be blank");
        }
        return key;
    }

    private static String valueMessage(String key) {
        return "connector option '" + key + "' must not have a null value";
    }
}
