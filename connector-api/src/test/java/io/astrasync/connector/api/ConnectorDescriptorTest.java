package io.astrasync.connector.api;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.EnumSet;
import java.util.Set;
import org.junit.jupiter.api.Test;

class ConnectorDescriptorTest {
    @Test
    void acceptsConsistentSourceAndSinkMetadata() {
        ConnectorDescriptor descriptor = new ConnectorDescriptor(
                "jdbc.v1",
                "1.2.3",
                Set.of(ConnectorRole.SOURCE, ConnectorRole.SINK),
                Set.of(Capability.BATCH_READ, Capability.BATCH_WRITE, Capability.TRANSACTIONAL_COMMIT));

        assertThat(descriptor.name()).isEqualTo("jdbc.v1");
        assertThat(descriptor.roles()).containsExactly(ConnectorRole.SOURCE, ConnectorRole.SINK);
        assertThat(descriptor.capabilities())
                .containsExactly(Capability.BATCH_READ, Capability.BATCH_WRITE, Capability.TRANSACTIONAL_COMMIT);
    }

    @Test
    void defensivelyCopiesAndProtectsRoleAndCapabilitySets() {
        EnumSet<ConnectorRole> roles = EnumSet.of(ConnectorRole.SOURCE);
        EnumSet<Capability> capabilities = EnumSet.of(Capability.BATCH_READ);

        ConnectorDescriptor descriptor = new ConnectorDescriptor("csv", "1", roles, capabilities);
        roles.add(ConnectorRole.SINK);
        capabilities.add(Capability.BATCH_WRITE);

        assertThat(descriptor.roles()).containsExactly(ConnectorRole.SOURCE);
        assertThat(descriptor.capabilities()).containsExactly(Capability.BATCH_READ);
        assertThatThrownBy(() -> descriptor.roles().add(ConnectorRole.SINK))
                .isInstanceOf(UnsupportedOperationException.class);
    }

    @Test
    void rejectsNonCanonicalNames() {
        for (String name : Set.of("CSV", "csv connector", ".csv", "csv-", "", "csv@1")) {
            assertThatThrownBy(() -> source(name))
                    .as("name %s", name)
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("name");
        }
        assertThatThrownBy(() -> source("a".repeat(129)))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("name");
    }

    @Test
    void rejectsMissingIdentityOrRoles() {
        assertThatThrownBy(() ->
                        new ConnectorDescriptor(null, "1", Set.of(ConnectorRole.SOURCE), Set.of(Capability.BATCH_READ)))
                .isInstanceOf(NullPointerException.class)
                .hasMessageContaining("name");
        assertThatThrownBy(() -> new ConnectorDescriptor(
                        "csv", " ", Set.of(ConnectorRole.SOURCE), Set.of(Capability.BATCH_READ)))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("version");
        assertThatThrownBy(() -> new ConnectorDescriptor("csv", "1", Set.of(), Set.of()))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("roles");
    }

    @Test
    void rejectsInconsistentRoleAndBatchCapabilitySets() {
        assertInconsistent(
                Set.of(ConnectorRole.SINK), Set.of(Capability.BATCH_READ, Capability.BATCH_WRITE), "BATCH_READ");
        assertInconsistent(
                Set.of(ConnectorRole.SOURCE), Set.of(Capability.BATCH_READ, Capability.BATCH_WRITE), "BATCH_WRITE");
        assertInconsistent(
                Set.of(ConnectorRole.SOURCE), Set.of(Capability.BATCH_READ, Capability.IDEMPOTENT_WRITE), "SINK");
        assertThatThrownBy(() ->
                        new ConnectorDescriptor("source", "1", Set.of(ConnectorRole.SOURCE), Set.of(Capability.UPSERT)))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("UPSERT")
                .hasMessageContaining("SINK");
        assertThatThrownBy(() ->
                        new ConnectorDescriptor("source", "1", Set.of(ConnectorRole.SOURCE), Set.of(Capability.DELETE)))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("DELETE")
                .hasMessageContaining("SINK");
    }

    @Test
    void allowsRolesThatOfferANonBatchMode() {
        ConnectorDescriptor descriptor =
                new ConnectorDescriptor("stream", "1", Set.of(ConnectorRole.SOURCE), Set.of(Capability.STREAM_READ));

        assertThat(descriptor.supportsRole(ConnectorRole.SOURCE)).isTrue();
        assertThat(descriptor.hasCapability(Capability.STREAM_READ)).isTrue();
        assertThat(descriptor.hasCapability(Capability.BATCH_READ)).isFalse();
    }

    @Test
    void rejectsNullSetsAndElements() {
        assertThatThrownBy(() -> new ConnectorDescriptor("csv", "1", null, Set.of(Capability.BATCH_READ)))
                .isInstanceOf(NullPointerException.class)
                .hasMessageContaining("roles");
        assertThatThrownBy(() -> new ConnectorDescriptor("csv", "1", Set.of(ConnectorRole.SOURCE), null))
                .isInstanceOf(NullPointerException.class)
                .hasMessageContaining("capabilities");
    }

    private static ConnectorDescriptor source(String name) {
        return new ConnectorDescriptor(name, "1", Set.of(ConnectorRole.SOURCE), Set.of(Capability.BATCH_READ));
    }

    private static void assertInconsistent(
            Set<ConnectorRole> roles, Set<Capability> capabilities, String expectedMessage) {
        assertThatThrownBy(() -> new ConnectorDescriptor("csv", "1", roles, capabilities))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining(expectedMessage);
    }
}
