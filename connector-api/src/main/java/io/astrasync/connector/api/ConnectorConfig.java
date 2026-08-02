package io.astrasync.connector.api;

public interface SourceConfig extends ConnectorConfig {
}

public interface SinkConfig extends ConnectorConfig {
}

public interface ConnectorConfig {

    String getString(String key);

    String getString(String key, String defaultValue);

    int getInt(String key);

    int getInt(String key, int defaultValue);

    long getLong(String key);

    long getLong(String key, long defaultValue);

    double getDouble(String key);

    double getDouble(String key, double defaultValue);

    boolean getBoolean(String key);

    boolean getBoolean(String key, boolean defaultValue);

    <T> T get(String key, Class<T> type);

    <T> T get(String key, T defaultValue);

    boolean contains(String key);
}
