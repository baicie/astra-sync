package io.astrasync.connector.api;

import java.util.ArrayList;
import java.util.Collection;
import java.util.Collections;
import java.util.EnumMap;
import java.util.EnumSet;
import java.util.HashSet;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.Set;
import java.util.TreeSet;
import java.util.regex.Pattern;

/** Static connector metadata used by catalog publication and side-effect-free planning. */
public final class ConnectorDescriptor {
    public static final int SCHEMA_VERSION = 1;

    private static final Pattern NAME_PATTERN = Pattern.compile("(?=.{1,128}$)[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?");
    private static final Pattern DESCRIPTION_KEY_PATTERN = Pattern.compile("[a-z0-9][a-z0-9._-]{0,255}");

    private final String name;
    private final String version;
    private final String displayName;
    private final String descriptionKey;
    private final Set<ConnectorRole> roles;
    private final Set<Capability> capabilities;
    private final List<ConnectorOptionDescriptor> options;
    private final List<ConnectorOptionPrefixDescriptor> optionPrefixes;
    private final Map<ConnectorRole, ConnectionRequirement> connectionRequirements;
    private final String connectionSchemaRevision;
    private final Set<String> acceptedConnectionSchemaRevisions;
    private final String descriptorRevision;

    public ConnectorDescriptor(String name, String version, Set<ConnectorRole> roles, Set<Capability> capabilities) {
        this(
                name,
                version,
                name,
                name == null ? null : "connector." + name + ".description",
                roles,
                capabilities,
                List.of(),
                List.of(),
                Map.of(),
                Set.of());
    }

    private ConnectorDescriptor(
            String name,
            String version,
            String displayName,
            String descriptionKey,
            Set<ConnectorRole> roles,
            Set<Capability> capabilities,
            Collection<ConnectorOptionDescriptor> options,
            Collection<ConnectorOptionPrefixDescriptor> optionPrefixes,
            Map<ConnectorRole, ConnectionRequirement> connectionRequirements,
            Set<String> acceptedPreviousConnectionSchemaRevisions) {
        this.name = checkedName(name);
        this.version = checkedText(version, "version", 128);
        this.displayName = checkedText(displayName, "displayName", 128);
        this.descriptionKey = checkedDescriptionKey(descriptionKey);
        this.roles = immutableEnumSet(roles, ConnectorRole.class, "roles");
        this.capabilities = immutableEnumSet(capabilities, Capability.class, "capabilities");
        if (this.roles.isEmpty()) {
            throw new IllegalArgumentException("roles must not be empty");
        }
        validateCapabilities(this.roles, this.capabilities);
        this.options = checkedOptions(options, this.roles);
        this.optionPrefixes = checkedPrefixes(optionPrefixes, this.roles, this.options);
        this.connectionRequirements =
                checkedRequirements(connectionRequirements, this.roles, this.options, this.optionPrefixes);
        this.connectionSchemaRevision = ConnectorRevisions.connectionSchemaRevision(
                this.name, this.options, this.optionPrefixes, this.connectionRequirements);
        TreeSet<String> accepted = new TreeSet<>();
        for (String revision : Objects.requireNonNull(
                acceptedPreviousConnectionSchemaRevisions,
                "acceptedPreviousConnectionSchemaRevisions must not be null")) {
            accepted.add(ConnectorRevisions.checkedRevision(revision));
        }
        accepted.add(connectionSchemaRevision);
        this.acceptedConnectionSchemaRevisions = Collections.unmodifiableSet(accepted);
        this.descriptorRevision = ConnectorRevisions.descriptorRevision(this);
    }

    public static Builder builder(String name, String version, Set<ConnectorRole> roles, Set<Capability> capabilities) {
        return new Builder(name, version, roles, capabilities);
    }

    public int descriptorSchemaVersion() {
        return SCHEMA_VERSION;
    }

    public String name() {
        return name;
    }

    public String version() {
        return version;
    }

    public String displayName() {
        return displayName;
    }

    public String descriptionKey() {
        return descriptionKey;
    }

    public Set<ConnectorRole> roles() {
        return roles;
    }

    public Set<Capability> capabilities() {
        return capabilities;
    }

    public List<ConnectorOptionDescriptor> options() {
        return options;
    }

    public List<ConnectorOptionPrefixDescriptor> optionPrefixes() {
        return optionPrefixes;
    }

    public Map<ConnectorRole, ConnectionRequirement> connectionRequirements() {
        return connectionRequirements;
    }

    public ConnectionRequirement connectionRequirement(ConnectorRole role) {
        return connectionRequirements.getOrDefault(
                Objects.requireNonNull(role, "role must not be null"), ConnectionRequirement.NONE);
    }

    public String connectionSchemaRevision() {
        return connectionSchemaRevision;
    }

    public Set<String> acceptedConnectionSchemaRevisions() {
        return acceptedConnectionSchemaRevisions;
    }

    public String descriptorRevision() {
        return descriptorRevision;
    }

    public boolean supportsRole(ConnectorRole role) {
        return roles.contains(Objects.requireNonNull(role, "role must not be null"));
    }

    public boolean hasCapability(Capability capability) {
        return capabilities.contains(Objects.requireNonNull(capability, "capability must not be null"));
    }

    public boolean acceptsConnectionSchema(String revision) {
        return revision != null && acceptedConnectionSchemaRevisions.contains(revision);
    }

    @Override
    public String toString() {
        return "ConnectorDescriptor[name=" + name + ", version=" + version + ", descriptorRevision="
                + descriptorRevision + "]";
    }

    @Override
    public boolean equals(Object value) {
        if (this == value) {
            return true;
        }
        if (!(value instanceof ConnectorDescriptor other)) {
            return false;
        }
        return name.equals(other.name)
                && version.equals(other.version)
                && displayName.equals(other.displayName)
                && descriptionKey.equals(other.descriptionKey)
                && roles.equals(other.roles)
                && capabilities.equals(other.capabilities)
                && options.equals(other.options)
                && optionPrefixes.equals(other.optionPrefixes)
                && connectionRequirements.equals(other.connectionRequirements)
                && acceptedConnectionSchemaRevisions.equals(other.acceptedConnectionSchemaRevisions);
    }

    @Override
    public int hashCode() {
        return Objects.hash(
                name,
                version,
                displayName,
                descriptionKey,
                roles,
                capabilities,
                options,
                optionPrefixes,
                connectionRequirements,
                acceptedConnectionSchemaRevisions);
    }

    private static String checkedName(String value) {
        Objects.requireNonNull(value, "name must not be null");
        if (!NAME_PATTERN.matcher(value).matches()) {
            throw new IllegalArgumentException(
                    "name must contain only lowercase letters, digits, dots, underscores, and hyphens"
                            + " and must start and end with a letter or digit: "
                            + value);
        }
        return value;
    }

    private static String checkedText(String value, String label, int maximumLength) {
        Objects.requireNonNull(value, label + " must not be null");
        if (value.isBlank() || value.length() > maximumLength) {
            throw new IllegalArgumentException(label + " must be between 1 and " + maximumLength + " characters");
        }
        return value;
    }

    private static String checkedDescriptionKey(String value) {
        Objects.requireNonNull(value, "descriptionKey must not be null");
        if (!DESCRIPTION_KEY_PATTERN.matcher(value).matches()) {
            throw new IllegalArgumentException("descriptionKey must be a canonical presentation key");
        }
        return value;
    }

    private static List<ConnectorOptionDescriptor> checkedOptions(
            Collection<ConnectorOptionDescriptor> values, Set<ConnectorRole> roles) {
        Objects.requireNonNull(values, "options must not be null");
        List<ConnectorOptionDescriptor> copy = new ArrayList<>(values.size());
        Set<String> keys = new HashSet<>();
        for (ConnectorOptionDescriptor value : values) {
            ConnectorOptionDescriptor option = Objects.requireNonNull(value, "options must not contain null");
            if (!roles.containsAll(option.roles())) {
                throw new IllegalArgumentException("option roles must be supported by the connector: " + option.key());
            }
            if (!keys.add(option.key())) {
                throw new IllegalArgumentException("duplicate connector option key: " + option.key());
            }
            copy.add(option);
        }
        copy.sort(java.util.Comparator.comparing(ConnectorOptionDescriptor::key));
        return List.copyOf(copy);
    }

    private static List<ConnectorOptionPrefixDescriptor> checkedPrefixes(
            Collection<ConnectorOptionPrefixDescriptor> values,
            Set<ConnectorRole> roles,
            List<ConnectorOptionDescriptor> options) {
        Objects.requireNonNull(values, "optionPrefixes must not be null");
        List<ConnectorOptionPrefixDescriptor> copy = new ArrayList<>(values.size());
        for (ConnectorOptionPrefixDescriptor value : values) {
            ConnectorOptionPrefixDescriptor prefix =
                    Objects.requireNonNull(value, "optionPrefixes must not contain null");
            if (!roles.containsAll(prefix.roles())) {
                throw new IllegalArgumentException("option prefix roles must be supported: " + prefix.prefix());
            }
            if (copy.stream()
                    .anyMatch(existing -> existing.prefix().startsWith(prefix.prefix())
                            || prefix.prefix().startsWith(existing.prefix()))) {
                throw new IllegalArgumentException("overlapping connector option prefix: " + prefix.prefix());
            }
            if (options.stream().anyMatch(option -> option.key().startsWith(prefix.prefix()))) {
                throw new IllegalArgumentException("option prefix overlaps an exact key: " + prefix.prefix());
            }
            copy.add(prefix);
        }
        copy.sort(java.util.Comparator.comparing(ConnectorOptionPrefixDescriptor::prefix));
        return List.copyOf(copy);
    }

    private static Map<ConnectorRole, ConnectionRequirement> checkedRequirements(
            Map<ConnectorRole, ConnectionRequirement> values,
            Set<ConnectorRole> roles,
            List<ConnectorOptionDescriptor> options,
            List<ConnectorOptionPrefixDescriptor> prefixes) {
        Objects.requireNonNull(values, "connectionRequirements must not be null");
        EnumMap<ConnectorRole, ConnectionRequirement> copy = new EnumMap<>(ConnectorRole.class);
        for (ConnectorRole role : ConnectorRole.values()) {
            copy.put(role, ConnectionRequirement.NONE);
        }
        values.forEach((role, requirement) -> {
            Objects.requireNonNull(role, "connectionRequirements must not contain a null role");
            Objects.requireNonNull(requirement, "connectionRequirements must not contain a null value");
            if (!roles.contains(role) && requirement != ConnectionRequirement.NONE) {
                throw new IllegalArgumentException("unsupported role cannot accept a Connection: " + role);
            }
            copy.put(role, requirement);
        });
        for (ConnectorOptionDescriptor option : options) {
            if (option.owner() != ConnectorOptionOwner.CONNECTION) {
                continue;
            }
            for (ConnectorRole role : option.roles()) {
                if (copy.get(role) == ConnectionRequirement.NONE) {
                    throw new IllegalArgumentException("Connection-owned option requires a Connection policy for role "
                            + role + ": " + option.key());
                }
                if (option.required() && copy.get(role) != ConnectionRequirement.REQUIRED) {
                    throw new IllegalArgumentException(
                            "required Connection-owned option requires a REQUIRED Connection for role " + role + ": "
                                    + option.key());
                }
            }
        }
        for (ConnectorOptionPrefixDescriptor prefix : prefixes) {
            if (prefix.owner() != ConnectorOptionOwner.CONNECTION) {
                continue;
            }
            for (ConnectorRole role : prefix.roles()) {
                if (copy.get(role) == ConnectionRequirement.NONE) {
                    throw new IllegalArgumentException(
                            "Connection-owned option prefix requires a Connection policy for role " + role + ": "
                                    + prefix.prefix());
                }
            }
        }
        for (ConnectorRole role : roles) {
            if (copy.get(role) != ConnectionRequirement.NONE
                    && options.stream()
                            .noneMatch(option -> option.owner() == ConnectorOptionOwner.CONNECTION
                                    && option.roles().contains(role))
                    && prefixes.stream()
                            .noneMatch(prefix -> prefix.owner() == ConnectorOptionOwner.CONNECTION
                                    && prefix.roles().contains(role))) {
                throw new IllegalArgumentException(
                        "Connection policy requires at least one Connection-owned option for role " + role);
            }
        }
        return Collections.unmodifiableMap(copy);
    }

    private static <E extends Enum<E>> Set<E> immutableEnumSet(Set<E> values, Class<E> elementType, String label) {
        Objects.requireNonNull(values, label + " must not be null");
        EnumSet<E> copy = EnumSet.noneOf(elementType);
        for (E value : values) {
            copy.add(Objects.requireNonNull(value, label + " must not contain null"));
        }
        return Collections.unmodifiableSet(copy);
    }

    private static void validateCapabilities(Set<ConnectorRole> roles, Set<Capability> capabilities) {
        validateRoleCapability(roles, capabilities, ConnectorRole.SOURCE, Capability.BATCH_READ);
        validateRoleCapability(roles, capabilities, ConnectorRole.SINK, Capability.BATCH_WRITE);
        validateRoleCapability(roles, capabilities, ConnectorRole.SOURCE, Capability.STREAM_READ);
        validateRoleCapability(roles, capabilities, ConnectorRole.SOURCE, Capability.REPLAYABLE_OFFSET);
        validateRoleCapability(roles, capabilities, ConnectorRole.SOURCE, Capability.CHANGE_DATA_CAPTURE);
        validateCdcCapabilities(capabilities);
        validateRoleCapability(roles, capabilities, ConnectorRole.SINK, Capability.UPSERT);
        validateRoleCapability(roles, capabilities, ConnectorRole.SINK, Capability.DELETE);
        validateRoleCapability(roles, capabilities, ConnectorRole.SINK, Capability.TRANSACTIONAL_COMMIT);
        validateRoleCapability(roles, capabilities, ConnectorRole.SINK, Capability.IDEMPOTENT_WRITE);
    }

    private static void validateRoleCapability(
            Set<ConnectorRole> roles, Set<Capability> capabilities, ConnectorRole role, Capability capability) {
        if (capabilities.contains(capability) && !roles.contains(role)) {
            throw new IllegalArgumentException(capability + " capability requires the " + role + " role");
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

    public static final class Builder {
        private final String name;
        private final String version;
        private final Set<ConnectorRole> roles;
        private final Set<Capability> capabilities;
        private String displayName;
        private String descriptionKey;
        private final List<ConnectorOptionDescriptor> options = new ArrayList<>();
        private final List<ConnectorOptionPrefixDescriptor> optionPrefixes = new ArrayList<>();
        private final Map<ConnectorRole, ConnectionRequirement> connectionRequirements =
                new EnumMap<>(ConnectorRole.class);
        private final Set<String> acceptedPreviousConnectionSchemaRevisions = new HashSet<>();

        private Builder(String name, String version, Set<ConnectorRole> roles, Set<Capability> capabilities) {
            this.name = name;
            this.version = version;
            this.roles = roles;
            this.capabilities = capabilities;
            this.displayName = name;
            this.descriptionKey = name == null ? null : "connector." + name + ".description";
        }

        public Builder displayName(String value) {
            displayName = value;
            return this;
        }

        public Builder descriptionKey(String value) {
            descriptionKey = value;
            return this;
        }

        public Builder option(ConnectorOptionDescriptor value) {
            options.add(value);
            return this;
        }

        public Builder optionPrefix(ConnectorOptionPrefixDescriptor value) {
            optionPrefixes.add(value);
            return this;
        }

        public Builder connectionRequirement(ConnectorRole role, ConnectionRequirement requirement) {
            connectionRequirements.put(role, requirement);
            return this;
        }

        public Builder acceptPreviousConnectionSchema(String revision) {
            acceptedPreviousConnectionSchemaRevisions.add(revision);
            return this;
        }

        public ConnectorDescriptor build() {
            return new ConnectorDescriptor(
                    name,
                    version,
                    displayName,
                    descriptionKey,
                    roles,
                    capabilities,
                    options,
                    optionPrefixes,
                    connectionRequirements,
                    acceptedPreviousConnectionSchemaRevisions);
        }
    }
}
