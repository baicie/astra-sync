package io.astrasync.connector.api;

import io.astrasync.control.v1.ConnectorDeliveryConstraint;
import io.astrasync.control.v1.ConnectorExecutionMode;
import io.astrasync.control.v1.ConnectorOptionDefinition;
import io.astrasync.control.v1.ConnectorOptionPrefix;
import io.astrasync.control.v1.ConnectorRoleConnectionRequirement;
import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.Map;

final class ConnectorProtobuf {
    private ConnectorProtobuf() {}

    static io.astrasync.control.v1.ConnectorDescriptor descriptor(ConnectorDescriptor source, boolean includeRevision) {
        io.astrasync.control.v1.ConnectorDescriptor.Builder target =
                io.astrasync.control.v1.ConnectorDescriptor.newBuilder()
                        .setDescriptorSchemaVersion(source.descriptorSchemaVersion())
                        .setName(source.name())
                        .setArtifactVersion(source.version())
                        .setDisplayName(source.displayName())
                        .setDescriptionKey(source.descriptionKey())
                        .setConnectionSchemaRevision(source.connectionSchemaRevision());
        if (includeRevision) {
            target.setDescriptorRevision(source.descriptorRevision());
        }
        source.roles().stream().map(ConnectorProtobuf::role).forEach(target::addRoles);
        source.capabilities().stream().map(ConnectorProtobuf::capability).forEach(target::addCapabilities);
        executionModes(source).forEach(target::addExecutionModes);
        deliveryConstraints(source).forEach(target::addDeliveryConstraints);
        source.options().stream().map(ConnectorProtobuf::option).forEach(target::addOptions);
        source.optionPrefixes().stream().map(ConnectorProtobuf::prefix).forEach(target::addOptionPrefixes);
        requirements(source.connectionRequirements()).forEach(target::addConnectionRequirements);
        source.acceptedConnectionSchemaRevisions().forEach(target::addAcceptedConnectionSchemaRevisions);
        return target.build();
    }

    static ConnectorOptionDefinition option(ConnectorOptionDescriptor source) {
        ConnectorOptionDefinition.Builder target = ConnectorOptionDefinition.newBuilder()
                .setKey(source.key())
                .setOwner(io.astrasync.control.v1.ConnectorOptionOwner.valueOf(
                        "CONNECTOR_OPTION_OWNER_" + source.owner().name()))
                .setValueType(io.astrasync.control.v1.ConnectorOptionType.valueOf(
                        "CONNECTOR_OPTION_TYPE_" + source.valueType().name()))
                .setRequired(source.required())
                .addAllEnumValues(source.enumValues())
                .setSensitivity(io.astrasync.control.v1.ConnectorOptionSensitivity.valueOf(
                        "CONNECTOR_OPTION_SENSITIVITY_" + source.sensitivity().name()))
                .setAdvanced(source.advanced())
                .setLabelKey(source.labelKey())
                .setHelpKey(source.helpKey());
        source.roles().stream().map(ConnectorProtobuf::role).forEach(target::addRoles);
        if (source.defaultValue() != null) {
            target.setDefaultValue(source.defaultValue());
        }
        if (source.minimum() != null) {
            target.setMinimum(source.minimum());
        }
        if (source.maximum() != null) {
            target.setMaximum(source.maximum());
        }
        if (source.minLength() != null) {
            target.setMinLength(source.minLength());
        }
        if (source.maxLength() != null) {
            target.setMaxLength(source.maxLength());
        }
        if (source.patternKey() != null) {
            target.setPatternKey(source.patternKey());
        }
        return target.build();
    }

    static ConnectorOptionPrefix prefix(ConnectorOptionPrefixDescriptor source) {
        ConnectorOptionPrefix.Builder target = ConnectorOptionPrefix.newBuilder()
                .setPrefix(source.prefix())
                .setOwner(io.astrasync.control.v1.ConnectorOptionOwner.valueOf(
                        "CONNECTOR_OPTION_OWNER_" + source.owner().name()))
                .setSensitivity(io.astrasync.control.v1.ConnectorOptionSensitivity.valueOf(
                        "CONNECTOR_OPTION_SENSITIVITY_" + source.sensitivity().name()))
                .setMaxEntries(source.maxEntries())
                .setMaxValueLength(source.maxValueLength())
                .setKeyPattern(source.keyPattern());
        source.roles().stream().map(ConnectorProtobuf::role).forEach(target::addRoles);
        return target.build();
    }

    static List<ConnectorRoleConnectionRequirement> requirements(
            Map<ConnectorRole, ConnectionRequirement> requirements) {
        List<ConnectorRoleConnectionRequirement> result = new ArrayList<>();
        requirements.entrySet().stream()
                .sorted(Map.Entry.comparingByKey())
                .forEach(entry -> result.add(ConnectorRoleConnectionRequirement.newBuilder()
                        .setRole(role(entry.getKey()))
                        .setRequirement(io.astrasync.control.v1.ConnectionRequirement.valueOf(
                                "CONNECTION_REQUIREMENT_" + entry.getValue().name()))
                        .build()));
        return List.copyOf(result);
    }

    private static Collection<ConnectorExecutionMode> executionModes(ConnectorDescriptor descriptor) {
        List<ConnectorExecutionMode> modes = new ArrayList<>(2);
        if (descriptor.hasCapability(Capability.BATCH_READ) || descriptor.hasCapability(Capability.BATCH_WRITE)) {
            modes.add(ConnectorExecutionMode.CONNECTOR_EXECUTION_MODE_BATCH);
        }
        if (descriptor.hasCapability(Capability.CHANGE_DATA_CAPTURE)
                || (descriptor.hasCapability(Capability.UPSERT) && descriptor.hasCapability(Capability.DELETE))) {
            modes.add(ConnectorExecutionMode.CONNECTOR_EXECUTION_MODE_CDC);
        }
        return modes;
    }

    private static Collection<ConnectorDeliveryConstraint> deliveryConstraints(ConnectorDescriptor descriptor) {
        List<ConnectorDeliveryConstraint> constraints = new ArrayList<>();
        constraints.add(ConnectorDeliveryConstraint.CONNECTOR_DELIVERY_CONSTRAINT_AT_MOST_ONCE);
        if (descriptor.hasCapability(Capability.REPLAYABLE_OFFSET)) {
            constraints.add(ConnectorDeliveryConstraint.CONNECTOR_DELIVERY_CONSTRAINT_REPLAYABLE_SOURCE);
        }
        if (descriptor.hasCapability(Capability.EXACTLY_ONCE_SOURCE)) {
            constraints.add(ConnectorDeliveryConstraint.CONNECTOR_DELIVERY_CONSTRAINT_EXACTLY_ONCE_SOURCE);
        }
        if (descriptor.hasCapability(Capability.IDEMPOTENT_WRITE)) {
            constraints.add(ConnectorDeliveryConstraint.CONNECTOR_DELIVERY_CONSTRAINT_IDEMPOTENT_SINK);
        }
        if (descriptor.hasCapability(Capability.TRANSACTIONAL_COMMIT)) {
            constraints.add(ConnectorDeliveryConstraint.CONNECTOR_DELIVERY_CONSTRAINT_TRANSACTIONAL_SINK);
        }
        return constraints;
    }

    private static io.astrasync.control.v1.ConnectorRole role(ConnectorRole source) {
        return io.astrasync.control.v1.ConnectorRole.valueOf("CONNECTOR_ROLE_" + source.name());
    }

    private static io.astrasync.control.v1.ConnectorCapability capability(Capability source) {
        return io.astrasync.control.v1.ConnectorCapability.valueOf("CONNECTOR_CAPABILITY_" + source.name());
    }
}
