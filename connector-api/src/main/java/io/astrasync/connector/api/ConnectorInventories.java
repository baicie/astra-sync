package io.astrasync.connector.api;

import io.astrasync.control.v1.ConnectorInventory;
import java.util.Collection;
import java.util.List;
import java.util.Objects;

/** Builds the deployment-authoritative connector inventory published to the control plane. */
public final class ConnectorInventories {
    public static final int SCHEMA_VERSION = 1;

    private ConnectorInventories() {}

    public static ConnectorInventory create(
            Collection<ConnectorDescriptor> descriptors,
            String jobSpecSchemaRevision,
            String compilerBuild,
            String executionProfile) {
        List<ConnectorDescriptor> ordered = ConnectorRevisions.orderedInventory(descriptors);
        String inventoryRevision = ConnectorRevisions.inventoryRevision(ordered);
        String compilerRevision =
                ConnectorRevisions.compilerRevision(ordered, jobSpecSchemaRevision, compilerBuild, executionProfile);
        ConnectorInventory.Builder inventory = ConnectorInventory.newBuilder()
                .setInventorySchemaVersion(SCHEMA_VERSION)
                .setInventoryRevision(inventoryRevision)
                .setCompilerRevision(compilerRevision)
                .setJobSpecSchemaRevision(checkedText(jobSpecSchemaRevision, "jobSpecSchemaRevision"))
                .setCompilerBuild(checkedText(compilerBuild, "compilerBuild"))
                .setExecutionProfile(checkedText(executionProfile, "executionProfile"));
        ordered.stream()
                .map(descriptor -> ConnectorProtobuf.descriptor(descriptor, true))
                .forEach(inventory::addDescriptors);
        return inventory.build();
    }

    public static byte[] deterministicBytes(ConnectorInventory inventory) {
        return ConnectorRevisions.deterministicBytes(Objects.requireNonNull(inventory, "inventory must not be null"));
    }

    private static String checkedText(String value, String label) {
        Objects.requireNonNull(value, label + " must not be null");
        if (value.isBlank() || value.length() > 256) {
            throw new IllegalArgumentException(label + " must contain between 1 and 256 characters");
        }
        return value;
    }
}
