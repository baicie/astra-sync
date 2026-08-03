package io.astrasync.connector.file;

import io.astrasync.connector.api.Capability;
import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.ConnectorDescriptor;
import io.astrasync.connector.api.ConnectorFactory;
import io.astrasync.connector.api.ConnectorRole;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import java.util.Set;

/** Creates strict UTF-8 RFC 4180 file connectors. */
public final class CsvConnectorFactory implements ConnectorFactory {
    public static final String CONNECTOR_NAME = "csv";
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
        return new CsvBatchSource(CsvConnectorOptions.source(configuration));
    }

    @Override
    public BatchSink createSink(ConnectorConfiguration configuration) {
        return new CsvBatchSink(CsvConnectorOptions.sink(configuration));
    }
}
