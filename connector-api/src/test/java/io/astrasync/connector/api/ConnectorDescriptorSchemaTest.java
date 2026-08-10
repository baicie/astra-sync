package io.astrasync.connector.api;

import static io.astrasync.connector.api.Capability.BATCH_READ;
import static io.astrasync.connector.api.ConnectionRequirement.OPTIONAL;
import static io.astrasync.connector.api.ConnectionRequirement.REQUIRED;
import static io.astrasync.connector.api.ConnectorOptionOwner.CONNECTION;
import static io.astrasync.connector.api.ConnectorOptionOwner.JOB;
import static io.astrasync.connector.api.ConnectorOptionSensitivity.PUBLIC;
import static io.astrasync.connector.api.ConnectorOptionSensitivity.RESTRICTED;
import static io.astrasync.connector.api.ConnectorOptionSensitivity.SECRET;
import static io.astrasync.connector.api.ConnectorOptionType.ENUM;
import static io.astrasync.connector.api.ConnectorOptionType.INTEGER;
import static io.astrasync.connector.api.ConnectorOptionType.STRING;
import static io.astrasync.connector.api.ConnectorRole.SOURCE;
import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.List;
import java.util.Set;
import org.junit.jupiter.api.Test;

class ConnectorDescriptorSchemaTest {
    @Test
    void buildsAnImmutableSortedConnectionSchema() {
        ConnectorDescriptor descriptor = ConnectorDescriptor.builder(
                        "database", "1.0.0", Set.of(SOURCE), Set.of(BATCH_READ))
                .displayName("Database")
                .option(jobOption("table"))
                .option(connectionOption("hostname", RESTRICTED, true))
                .option(connectionOption("password", SECRET, true))
                .connectionRequirement(SOURCE, REQUIRED)
                .build();

        assertThat(descriptor.options())
                .extracting(ConnectorOptionDescriptor::key)
                .containsExactly("hostname", "password", "table");
        assertThat(descriptor.connectionRequirement(SOURCE)).isEqualTo(REQUIRED);
        assertThat(descriptor.acceptedConnectionSchemaRevisions())
                .containsExactly(descriptor.connectionSchemaRevision());
        assertThat(descriptor.descriptorRevision()).matches("sha256:[0-9a-f]{64}");
        assertThatThrownBy(() -> descriptor.options().add(jobOption("query")))
                .isInstanceOf(UnsupportedOperationException.class);
    }

    @Test
    void revisionsAreDeterministicAndPreserveOrderedEnumSemantics() {
        ConnectorDescriptor first = descriptorWithSnapshotModes("initial", "never");
        ConnectorDescriptor reorderedInput = ConnectorDescriptor.builder(
                        "database", "1.0.0", Set.of(SOURCE), Set.of(BATCH_READ))
                .option(jobOption("zeta"))
                .option(jobOption("alpha"))
                .build();
        ConnectorDescriptor canonicalInput = ConnectorDescriptor.builder(
                        "database", "1.0.0", Set.of(SOURCE), Set.of(BATCH_READ))
                .option(jobOption("alpha"))
                .option(jobOption("zeta"))
                .build();

        assertThat(reorderedInput.descriptorRevision()).isEqualTo(canonicalInput.descriptorRevision());
        assertThat(descriptorWithSnapshotModes("initial", "never").descriptorRevision())
                .isEqualTo(first.descriptorRevision());
        assertThat(descriptorWithSnapshotModes("never", "initial").descriptorRevision())
                .isNotEqualTo(first.descriptorRevision());
    }

    @Test
    void presentationAndJobOnlyChangesDoNotChangeConnectionSchemaRevision() {
        ConnectorDescriptor original = databaseDescriptor("Database", "table");
        ConnectorDescriptor presentationChange = databaseDescriptor("Database source", "query");

        assertThat(presentationChange.connectionSchemaRevision()).isEqualTo(original.connectionSchemaRevision());
        assertThat(presentationChange.descriptorRevision()).isNotEqualTo(original.descriptorRevision());
    }

    @Test
    void acceptsExplicitPreviousConnectionSchemaRevision() {
        String previous = "sha256:" + "1".repeat(64);
        ConnectorDescriptor descriptor = ConnectorDescriptor.builder(
                        "database", "2.0.0", Set.of(SOURCE), Set.of(BATCH_READ))
                .option(connectionOption("hostname", RESTRICTED, true))
                .connectionRequirement(SOURCE, REQUIRED)
                .acceptPreviousConnectionSchema(previous)
                .build();

        assertThat(descriptor.acceptsConnectionSchema(previous)).isTrue();
        assertThat(descriptor.acceptsConnectionSchema("invalid")).isFalse();
    }

    @Test
    void rejectsDuplicateAndOverlappingKeys() {
        assertThatThrownBy(() -> ConnectorDescriptor.builder("database", "1.0.0", Set.of(SOURCE), Set.of(BATCH_READ))
                        .option(jobOption("table"))
                        .option(jobOption("table"))
                        .build())
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("duplicate");

        ConnectorOptionPrefixDescriptor prefix = new ConnectorOptionPrefixDescriptor(
                "vendor.", Set.of(SOURCE), JOB, PUBLIC, 8, 256, "[A-Za-z][A-Za-z0-9.-]{0,63}");
        assertThatThrownBy(() -> ConnectorDescriptor.builder("database", "1.0.0", Set.of(SOURCE), Set.of(BATCH_READ))
                        .option(jobOption("vendor.mode"))
                        .optionPrefix(prefix)
                        .build())
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("overlaps");
    }

    @Test
    void rejectsUnsafeSensitiveDefinitions() {
        assertThatThrownBy(() -> ConnectorOptionDescriptor.builder("password", STRING, JOB, SOURCE)
                        .sensitivity(SECRET)
                        .build())
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("owned by CONNECTION");
        assertThatThrownBy(() -> ConnectorOptionDescriptor.builder("hostname", STRING, JOB, SOURCE)
                        .sensitivity(RESTRICTED)
                        .build())
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("owned by CONNECTION");
        assertThatThrownBy(() -> ConnectorOptionDescriptor.builder("password", STRING, CONNECTION, SOURCE)
                        .sensitivity(SECRET)
                        .defaultValue("unsafe")
                        .build())
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("default value");
    }

    @Test
    void rejectsConnectionOwnershipWithoutAConsistentRolePolicy() {
        assertThatThrownBy(() -> ConnectorDescriptor.builder("database", "1.0.0", Set.of(SOURCE), Set.of(BATCH_READ))
                        .option(connectionOption("hostname", RESTRICTED, false))
                        .build())
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("Connection policy");
        assertThatThrownBy(() -> ConnectorDescriptor.builder("database", "1.0.0", Set.of(SOURCE), Set.of(BATCH_READ))
                        .option(connectionOption("hostname", RESTRICTED, true))
                        .connectionRequirement(SOURCE, OPTIONAL)
                        .build())
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("REQUIRED Connection");
    }

    @Test
    void validatesTypedDefaultsAndBounds() {
        assertThatThrownBy(() -> ConnectorOptionDescriptor.builder("port", INTEGER, JOB, SOURCE)
                        .numericBounds(1, 65_535)
                        .defaultValue("not-a-number")
                        .build())
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("must be an integer");
        assertThatThrownBy(() -> ConnectorOptionDescriptor.builder("mode", ENUM, JOB, SOURCE)
                        .enumValues("one", "two")
                        .defaultValue("three")
                        .build())
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("one of enumValues");
    }

    @Test
    void inventoryRevisionRejectsDuplicateNames() {
        ConnectorDescriptor first = new ConnectorDescriptor("database", "1", Set.of(SOURCE), Set.of(BATCH_READ));
        ConnectorDescriptor second = new ConnectorDescriptor("database", "2", Set.of(SOURCE), Set.of(BATCH_READ));

        assertThatThrownBy(() -> ConnectorRevisions.inventoryRevision(List.of(first, second)))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("duplicate names");
    }

    private static ConnectorDescriptor databaseDescriptor(String displayName, String jobKey) {
        return ConnectorDescriptor.builder("database", "1.0.0", Set.of(SOURCE), Set.of(BATCH_READ))
                .displayName(displayName)
                .option(connectionOption("hostname", RESTRICTED, true))
                .option(jobOption(jobKey))
                .connectionRequirement(SOURCE, REQUIRED)
                .build();
    }

    private static ConnectorDescriptor descriptorWithSnapshotModes(String... modes) {
        return ConnectorDescriptor.builder("database", "1.0.0", Set.of(SOURCE), Set.of(BATCH_READ))
                .option(ConnectorOptionDescriptor.builder("snapshotMode", ENUM, JOB, SOURCE)
                        .enumValues(modes)
                        .defaultValue(modes[0])
                        .build())
                .build();
    }

    private static ConnectorOptionDescriptor jobOption(String key) {
        return ConnectorOptionDescriptor.builder(key, STRING, JOB, SOURCE).build();
    }

    private static ConnectorOptionDescriptor connectionOption(
            String key, ConnectorOptionSensitivity sensitivity, boolean required) {
        ConnectorOptionDescriptor.Builder builder = ConnectorOptionDescriptor.builder(key, STRING, CONNECTION, SOURCE)
                .sensitivity(sensitivity);
        if (required) {
            builder.required();
        }
        return builder.build();
    }
}
