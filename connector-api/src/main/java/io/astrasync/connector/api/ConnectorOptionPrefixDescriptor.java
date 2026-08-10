package io.astrasync.connector.api;

import java.util.Collections;
import java.util.EnumSet;
import java.util.Objects;
import java.util.Set;
import java.util.regex.Pattern;

/** Bounded policy for a connector-owned extension option namespace. */
public record ConnectorOptionPrefixDescriptor(
        String prefix,
        Set<ConnectorRole> roles,
        ConnectorOptionOwner owner,
        ConnectorOptionSensitivity sensitivity,
        int maxEntries,
        int maxValueLength,
        String keyPattern) {
    private static final Pattern PREFIX_PATTERN = Pattern.compile("[A-Za-z][A-Za-z0-9_-]{0,63}\\.");

    public ConnectorOptionPrefixDescriptor {
        Objects.requireNonNull(prefix, "prefix must not be null");
        if (!PREFIX_PATTERN.matcher(prefix).matches()) {
            throw new IllegalArgumentException("prefix must be a canonical option namespace");
        }
        Objects.requireNonNull(roles, "roles must not be null");
        EnumSet<ConnectorRole> roleCopy = EnumSet.noneOf(ConnectorRole.class);
        for (ConnectorRole role : roles) {
            roleCopy.add(Objects.requireNonNull(role, "roles must not contain null"));
        }
        if (roleCopy.isEmpty()) {
            throw new IllegalArgumentException("roles must not be empty");
        }
        roles = Collections.unmodifiableSet(roleCopy);
        owner = Objects.requireNonNull(owner, "owner must not be null");
        sensitivity = Objects.requireNonNull(sensitivity, "sensitivity must not be null");
        if (sensitivity != ConnectorOptionSensitivity.PUBLIC && owner != ConnectorOptionOwner.CONNECTION) {
            throw new IllegalArgumentException("sensitive option prefix must be owned by CONNECTION");
        }
        if (maxEntries <= 0 || maxEntries > 256 || maxValueLength <= 0 || maxValueLength > 65_536) {
            throw new IllegalArgumentException("option prefix bounds are invalid");
        }
        Objects.requireNonNull(keyPattern, "keyPattern must not be null");
        Pattern.compile(keyPattern);
    }
}
