package io.astrasync.connector.api;

import java.util.Map;

public interface Configurable {

    void configure(Map<String, String> config);

    Map<String, String> getConfig();
}
