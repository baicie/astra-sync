package io.astrasync.connector.jdbc;

import io.astrasync.connector.api.Capability;
import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.ConnectorDescriptor;
import io.astrasync.connector.api.ConnectorFactory;
import io.astrasync.connector.api.ConnectorRole;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import java.util.Set;

/** Creates generic JDBC Source and Sink instances. */
public final class JdbcConnectorFactory implements ConnectorFactory {
    public static final String CONNECTOR_NAME = "jdbc";
    public static final String CONNECTOR_VERSION = "1.0.0";

    private static final ConnectorDescriptor DESCRIPTOR = new ConnectorDescriptor(
            CONNECTOR_NAME,
            CONNECTOR_VERSION,
            Set.of(ConnectorRole.SOURCE, ConnectorRole.SINK),
            Set.of(Capability.BATCH_READ, Capability.BATCH_WRITE));

    @Override
    public ConnectorDescriptor descriptor() {
        return DESCRIPTOR;
    }

    @Override
    public BatchSource createSource(ConnectorConfiguration configuration) {
        return new JdbcBatchSource(JdbcConnectorOptions.source(configuration));
    }

    @Override
    public BatchSink createSink(ConnectorConfiguration configuration) {
        return new JdbcBatchSink(JdbcConnectorOptions.sink(configuration));
    }
}
