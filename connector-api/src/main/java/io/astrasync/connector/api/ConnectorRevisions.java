package io.astrasync.connector.api;

import com.google.protobuf.CodedOutputStream;
import com.google.protobuf.MessageLite;
import io.astrasync.control.v1.ConnectorCompilerIdentity;
import io.astrasync.control.v1.ConnectorConnectionSchemaIdentity;
import io.astrasync.control.v1.ConnectorInventoryEntry;
import io.astrasync.control.v1.ConnectorInventoryIdentity;
import java.io.ByteArrayOutputStream;
import java.io.IOException;
import java.security.MessageDigest;
import java.security.NoSuchAlgorithmException;
import java.util.Collection;
import java.util.Comparator;
import java.util.List;
import java.util.Map;
import java.util.Objects;
import java.util.regex.Pattern;

/** Deterministic protobuf revision identifiers for connector descriptors and inventories. */
public final class ConnectorRevisions {
    private static final Pattern REVISION_PATTERN = Pattern.compile("sha256:[0-9a-f]{64}");

    private ConnectorRevisions() {}

    public static String descriptorRevision(ConnectorDescriptor descriptor) {
        Objects.requireNonNull(descriptor, "descriptor must not be null");
        return digest(ConnectorProtobuf.descriptor(descriptor, false));
    }

    public static String connectionSchemaRevision(
            String connectorName,
            List<ConnectorOptionDescriptor> options,
            List<ConnectorOptionPrefixDescriptor> prefixes,
            Map<ConnectorRole, ConnectionRequirement> requirements) {
        Objects.requireNonNull(connectorName, "connectorName must not be null");
        Objects.requireNonNull(options, "options must not be null");
        Objects.requireNonNull(prefixes, "prefixes must not be null");
        Objects.requireNonNull(requirements, "requirements must not be null");
        ConnectorConnectionSchemaIdentity.Builder identity =
                ConnectorConnectionSchemaIdentity.newBuilder().setConnectorName(connectorName);
        options.stream()
                .filter(option -> option.owner() == ConnectorOptionOwner.CONNECTION)
                .sorted(Comparator.comparing(ConnectorOptionDescriptor::key))
                .map(ConnectorProtobuf::option)
                .forEach(identity::addOptions);
        prefixes.stream()
                .filter(prefix -> prefix.owner() == ConnectorOptionOwner.CONNECTION)
                .sorted(Comparator.comparing(ConnectorOptionPrefixDescriptor::prefix))
                .map(ConnectorProtobuf::prefix)
                .forEach(identity::addOptionPrefixes);
        ConnectorProtobuf.requirements(requirements).forEach(identity::addConnectionRequirements);
        return digest(identity.build());
    }

    public static String inventoryRevision(Collection<ConnectorDescriptor> descriptors) {
        List<ConnectorDescriptor> ordered = orderedInventory(descriptors);
        ConnectorInventoryIdentity.Builder identity = ConnectorInventoryIdentity.newBuilder();
        for (ConnectorDescriptor descriptor : ordered) {
            identity.addEntries(ConnectorInventoryEntry.newBuilder()
                    .setName(descriptor.name())
                    .setArtifactVersion(descriptor.version())
                    .setDescriptorRevision(descriptor.descriptorRevision()));
        }
        return digest(identity.build());
    }

    public static String compilerRevision(
            Collection<ConnectorDescriptor> descriptors,
            String jobSpecSchema,
            String compilerBuild,
            String executionProfile) {
        ConnectorCompilerIdentity identity = ConnectorCompilerIdentity.newBuilder()
                .setInventoryRevision(inventoryRevision(descriptors))
                .setJobSpecSchemaRevision(checkedText(jobSpecSchema, "jobSpecSchema"))
                .setCompilerBuild(checkedText(compilerBuild, "compilerBuild"))
                .setExecutionProfile(checkedText(executionProfile, "executionProfile"))
                .build();
        return digest(identity);
    }

    static List<ConnectorDescriptor> orderedInventory(Collection<ConnectorDescriptor> descriptors) {
        Objects.requireNonNull(descriptors, "descriptors must not be null");
        List<ConnectorDescriptor> ordered = descriptors.stream()
                .map(descriptor -> Objects.requireNonNull(descriptor, "descriptors must not contain null"))
                .sorted(Comparator.comparing(ConnectorDescriptor::name).thenComparing(ConnectorDescriptor::version))
                .toList();
        if (ordered.stream().map(ConnectorDescriptor::name).distinct().count() != ordered.size()) {
            throw new IllegalArgumentException("connector inventory contains duplicate names");
        }
        return ordered;
    }

    static String checkedRevision(String revision) {
        Objects.requireNonNull(revision, "revision must not be null");
        if (!REVISION_PATTERN.matcher(revision).matches()) {
            throw new IllegalArgumentException("revision must be a lowercase sha256 identifier");
        }
        return revision;
    }

    static byte[] deterministicBytes(MessageLite message) {
        Objects.requireNonNull(message, "message must not be null");
        try {
            ByteArrayOutputStream bytes = new ByteArrayOutputStream(message.getSerializedSize());
            CodedOutputStream output = CodedOutputStream.newInstance(bytes);
            output.useDeterministicSerialization();
            message.writeTo(output);
            output.flush();
            return bytes.toByteArray();
        } catch (IOException exception) {
            throw new IllegalStateException("failed to serialize connector metadata", exception);
        }
    }

    private static String checkedText(String value, String label) {
        Objects.requireNonNull(value, label + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(label + " must not be blank");
        }
        return value;
    }

    private static String digest(MessageLite message) {
        try {
            byte[] digest = MessageDigest.getInstance("SHA-256").digest(deterministicBytes(message));
            return "sha256:" + java.util.HexFormat.of().formatHex(digest);
        } catch (NoSuchAlgorithmException exception) {
            throw new IllegalStateException("SHA-256 is not available", exception);
        }
    }
}
