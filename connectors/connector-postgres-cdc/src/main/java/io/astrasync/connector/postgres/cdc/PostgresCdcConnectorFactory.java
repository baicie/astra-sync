package io.astrasync.connector.postgres.cdc;

import io.astrasync.connector.api.Capability;
import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.ConnectorDescriptor;
import io.astrasync.connector.api.ConnectorFactory;
import io.astrasync.connector.api.ConnectorRole;
import io.astrasync.connector.api.source.CdcSource;
import java.util.Set;

/** Creates native PostgreSQL logical-replication sources backed by Debezium. */
public final class PostgresCdcConnectorFactory implements ConnectorFactory {
    public static final String CONNECTOR_NAME = "postgres-cdc";
    public static final String CONNECTOR_VERSION = "1.0.0";

    private static final ConnectorDescriptor DESCRIPTOR = new ConnectorDescriptor(
            CONNECTOR_NAME,
            CONNECTOR_VERSION,
            Set.of(ConnectorRole.SOURCE),
            Set.of(
                    Capability.STREAM_READ,
                    Capability.REPLAYABLE_OFFSET,
                    Capability.EXACTLY_ONCE_SOURCE,
                    Capability.CHANGE_DATA_CAPTURE,
                    Capability.SCHEMA_EVOLUTION,
                    Capability.FAULT_TOLERANCE));

    @Override
    public ConnectorDescriptor descriptor() {
        return DESCRIPTOR;
    }

    @Override
    public CdcSource createCdcSource(ConnectorConfiguration configuration) {
        PostgresCdcConnectorOptions options = PostgresCdcConnectorOptions.from(configuration);
        return new io.astrasync.connector.debezium.DebeziumCdcSource(
                options.identity(), options.properties(), options.queuedBatches(), options.commitTimeout());
    }
}
