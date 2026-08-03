package io.astrasync.connector.api;

import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.Set;
import org.junit.jupiter.api.Test;

class ConnectorFactoryTest {
    @Test
    void defaultSourceCreationRejectsUnsupportedRole() {
        ConnectorFactory factory = factory(
                new ConnectorDescriptor("sink-only", "1", Set.of(ConnectorRole.SINK), Set.of(Capability.BATCH_WRITE)));

        assertThatThrownBy(() -> factory.createSource(ConnectorConfiguration.empty()))
                .isInstanceOf(UnsupportedOperationException.class)
                .hasMessageContaining("sink-only")
                .hasMessageContaining("SOURCE");
    }

    @Test
    void defaultSinkCreationRejectsUnsupportedRole() {
        ConnectorFactory factory = factory(new ConnectorDescriptor(
                "source-only", "1", Set.of(ConnectorRole.SOURCE), Set.of(Capability.BATCH_READ)));

        assertThatThrownBy(() -> factory.createSink(ConnectorConfiguration.empty()))
                .isInstanceOf(UnsupportedOperationException.class)
                .hasMessageContaining("source-only")
                .hasMessageContaining("SINK");
    }

    private static ConnectorFactory factory(ConnectorDescriptor descriptor) {
        return () -> descriptor;
    }
}
