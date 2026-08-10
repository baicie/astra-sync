package io.astrasync.connector.api;

import static io.astrasync.connector.api.Capability.BATCH_READ;
import static io.astrasync.connector.api.Capability.CHANGE_DATA_CAPTURE;
import static io.astrasync.connector.api.Capability.EXACTLY_ONCE_SOURCE;
import static io.astrasync.connector.api.Capability.REPLAYABLE_OFFSET;
import static io.astrasync.connector.api.Capability.STREAM_READ;
import static io.astrasync.connector.api.ConnectorRole.SOURCE;
import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.control.v1.ConnectorDeliveryConstraint;
import io.astrasync.control.v1.ConnectorExecutionMode;
import io.astrasync.control.v1.ConnectorInventory;
import java.util.List;
import java.util.Set;
import org.junit.jupiter.api.Test;

class ConnectorInventoriesTest {
    @Test
    void emitsAnOrderedSelfConsistentDeterministicInventory() throws Exception {
        ConnectorDescriptor cdc = new ConnectorDescriptor(
                "mysql-cdc",
                "1.0.0",
                Set.of(SOURCE),
                Set.of(STREAM_READ, REPLAYABLE_OFFSET, EXACTLY_ONCE_SOURCE, CHANGE_DATA_CAPTURE));
        ConnectorDescriptor batch = new ConnectorDescriptor("csv", "1.0.0", Set.of(SOURCE), Set.of(BATCH_READ));

        ConnectorInventory inventory =
                ConnectorInventories.create(List.of(cdc, batch), "jobspec-v1", "build-42", "standard");
        byte[] first = ConnectorInventories.deterministicBytes(inventory);
        byte[] second = ConnectorInventories.deterministicBytes(
                ConnectorInventories.create(List.of(batch, cdc), "jobspec-v1", "build-42", "standard"));
        ConnectorInventory parsed = ConnectorInventory.parseFrom(first);

        assertThat(first).isEqualTo(second);
        assertThat(parsed.getInventoryRevision()).isEqualTo(ConnectorRevisions.inventoryRevision(List.of(batch, cdc)));
        assertThat(parsed.getCompilerRevision())
                .isEqualTo(
                        ConnectorRevisions.compilerRevision(List.of(batch, cdc), "jobspec-v1", "build-42", "standard"));
        assertThat(parsed.getDescriptorsList())
                .extracting(io.astrasync.control.v1.ConnectorDescriptor::getName)
                .containsExactly("csv", "mysql-cdc");
        assertThat(parsed.getDescriptors(1).getExecutionModesList())
                .containsExactly(ConnectorExecutionMode.CONNECTOR_EXECUTION_MODE_CDC);
        assertThat(parsed.getDescriptors(1).getDeliveryConstraintsList())
                .containsExactly(
                        ConnectorDeliveryConstraint.CONNECTOR_DELIVERY_CONSTRAINT_AT_MOST_ONCE,
                        ConnectorDeliveryConstraint.CONNECTOR_DELIVERY_CONSTRAINT_REPLAYABLE_SOURCE,
                        ConnectorDeliveryConstraint.CONNECTOR_DELIVERY_CONSTRAINT_EXACTLY_ONCE_SOURCE);
    }

    @Test
    void rejectsBlankCompilerIdentityInputs() {
        ConnectorDescriptor descriptor = new ConnectorDescriptor("csv", "1", Set.of(SOURCE), Set.of(BATCH_READ));

        assertThatThrownBy(() -> ConnectorInventories.create(List.of(descriptor), " ", "build", "standard"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("jobSpecSchema");
    }
}
