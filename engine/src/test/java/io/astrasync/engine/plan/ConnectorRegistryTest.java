package io.astrasync.engine.plan;

import static io.astrasync.connector.api.Capability.BATCH_READ;
import static io.astrasync.connector.api.ConnectorRole.SOURCE;
import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.ConnectorDescriptor;
import io.astrasync.connector.api.ConnectorFactory;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import java.util.Set;
import org.junit.jupiter.api.Test;

class ConnectorRegistryTest {
    @Test
    void storesDescriptorsInCanonicalNameOrder() {
        ConnectorRegistry registry = ConnectorRegistry.of(
                factory(descriptor("zeta", Set.of(SOURCE), Set.of(BATCH_READ))),
                factory(descriptor("alpha", Set.of(SOURCE), Set.of(BATCH_READ))));

        assertThat(registry.descriptors()).extracting(ConnectorDescriptor::name).containsExactly("alpha", "zeta");
        assertThat(registry.findFactory("alpha")).isPresent();
        assertThat(registry.findDescriptor("missing")).isEmpty();
    }

    @Test
    void rejectsDuplicateNamesRegardlessOfVersion() {
        assertThatThrownBy(() -> ConnectorRegistry.of(
                        factory(descriptor("csv", Set.of(SOURCE), Set.of(BATCH_READ))),
                        factory(new ConnectorDescriptor("csv", "2.0.0", Set.of(SOURCE), Set.of(BATCH_READ)))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("duplicate connector name: csv");
    }

    @Test
    void pinsMaterializationToTheCompiledDescriptorVersion() {
        ConnectorFactory factory = factory(descriptor("csv", Set.of(SOURCE), Set.of(BATCH_READ)));
        ConnectorRegistry registry = ConnectorRegistry.of(factory);

        assertThat(registry.requireFactory("csv", "1.0.0")).isSameAs(factory);
        assertThatThrownBy(() -> registry.requireFactory("csv", "2.0.0"))
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("expected 2.0.0, registered 1.0.0");
        assertThatThrownBy(() -> registry.requireFactory("missing", "1.0.0"))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("connector is not registered: missing");
    }

    private static ConnectorDescriptor descriptor(
            String name,
            Set<io.astrasync.connector.api.ConnectorRole> roles,
            Set<io.astrasync.connector.api.Capability> capabilities) {
        return new ConnectorDescriptor(name, "1.0.0", roles, capabilities);
    }

    private static ConnectorFactory factory(ConnectorDescriptor descriptor) {
        return new ConnectorFactory() {
            @Override
            public ConnectorDescriptor descriptor() {
                return descriptor;
            }

            @Override
            public BatchSource createSource(ConnectorConfiguration configuration) {
                return new BatchSource() {
                    @Override
                    public void open() {}

                    @Override
                    public RowBatch readBatch(int maxRows) {
                        return RowBatch.end();
                    }

                    @Override
                    public void close() {}
                };
            }

            @Override
            public BatchSink createSink(ConnectorConfiguration configuration) {
                return new BatchSink() {
                    @Override
                    public void open() {}

                    @Override
                    public void writeBatch(RowBatch batch) {}

                    @Override
                    public void close() {}
                };
            }
        };
    }
}
