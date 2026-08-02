package io.astrasync.connector.api;

import java.util.Map;

public interface ConnectorCapabilities {

    String getName();

    String getVersion();

    Map<Capability, Boolean> getCapabilities();

    Map<String, String> getProperties();

    default boolean hasCapability(Capability capability) {
        return Boolean.TRUE.equals(getCapabilities().get(capability));
    }
}
