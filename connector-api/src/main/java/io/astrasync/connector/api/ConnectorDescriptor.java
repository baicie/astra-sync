package io.astrasync.connector.api;

import java.util.Collections;
import java.util.EnumSet;
import java.util.Objects;
import java.util.Set;
import java.util.regex.Pattern;

/** Static connector metadata used during side-effect-free planning. */
public record ConnectorDescriptor(String name, String version, Set<ConnectorRole> roles, Set<Capability> capabilities) {
    private static final Pattern NAME_PATTERN = Pattern.compile("(?=.{1,128}$)[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?");

    public ConnectorDescriptor {
        Objects.requireNonNull(name, "name must not be null");
        if (!NAME_PATTERN.matcher(name).matches()) {
            throw new IllegalArgumentException(
                    "name must contain only lowercase letters, digits, dots, underscores, and hyphens"
                            + " and must start and end with a letter or digit: "
                            + name);
        }
        Objects.requireNonNull(version, "version must not be null");
        if (version.isBlank()) {
            throw new IllegalArgumentException("version must not be blank");
        }

        roles = immutableEnumSet(roles, ConnectorRole.class, "roles");
        capabilities = immutableEnumSet(capabilities, Capability.class, "capabilities");
        if (roles.isEmpty()) {
            throw new IllegalArgumentException("roles must not be empty");
        }
        validateBatchCapability(roles, capabilities, ConnectorRole.SOURCE, Capability.BATCH_READ);
        validateBatchCapability(roles, capabilities, ConnectorRole.SINK, Capability.BATCH_WRITE);
        validateBatchCapability(roles, capabilities, ConnectorRole.SOURCE, Capability.STREAM_READ);
        validateBatchCapability(roles, capabilities, ConnectorRole.SOURCE, Capability.REPLAYABLE_OFFSET);
        validateBatchCapability(roles, capabilities, ConnectorRole.SOURCE, Capability.CHANGE_DATA_CAPTURE);
        validateCdcCapabilities(capabilities);
        validateSinkCapability(roles, capabilities, Capability.UPSERT);
        validateSinkCapability(roles, capabilities, Capability.DELETE);
        validateSinkCapability(roles, capabilities, Capability.TRANSACTIONAL_COMMIT);
        validateSinkCapability(roles, capabilities, Capability.IDEMPOTENT_WRITE);
    }

    public boolean supportsRole(ConnectorRole role) {
        return roles.contains(Objects.requireNonNull(role, "role must not be null"));
    }

    public boolean hasCapability(Capability capability) {
        return capabilities.contains(Objects.requireNonNull(capability, "capability must not be null"));
    }

    private static void validateBatchCapability(
            Set<ConnectorRole> roles, Set<Capability> capabilities, ConnectorRole role, Capability capability) {
        if (capabilities.contains(capability) && !roles.contains(role)) {
            throw new IllegalArgumentException(capability + " capability requires the " + role + " role");
        }
    }

    private static void validateSinkCapability(
            Set<ConnectorRole> roles, Set<Capability> capabilities, Capability capability) {
        if (capabilities.contains(capability) && !roles.contains(ConnectorRole.SINK)) {
            throw new IllegalArgumentException(capability + " capability requires the SINK role");
        }
    }

    private static void validateCdcCapabilities(Set<Capability> capabilities) {
        if (capabilities.contains(Capability.CHANGE_DATA_CAPTURE)
                && (!capabilities.contains(Capability.STREAM_READ)
                        || !capabilities.contains(Capability.REPLAYABLE_OFFSET))) {
            throw new IllegalArgumentException(
                    "CHANGE_DATA_CAPTURE capability requires STREAM_READ and REPLAYABLE_OFFSET");
        }
        if (capabilities.contains(Capability.EXACTLY_ONCE_SOURCE)
                && !capabilities.contains(Capability.REPLAYABLE_OFFSET)) {
            throw new IllegalArgumentException("EXACTLY_ONCE_SOURCE capability requires REPLAYABLE_OFFSET");
        }
    }

    private static <E extends Enum<E>> Set<E> immutableEnumSet(Set<E> values, Class<E> elementType, String label) {
        Objects.requireNonNull(values, label + " must not be null");
        EnumSet<E> copy = EnumSet.noneOf(elementType);
        for (E value : values) {
            copy.add(Objects.requireNonNull(value, label + " must not contain null"));
        }
        return Collections.unmodifiableSet(copy);
    }
}
