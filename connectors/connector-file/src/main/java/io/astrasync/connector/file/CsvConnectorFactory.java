package io.astrasync.connector.file;

import io.astrasync.connector.api.Capability;
import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.ConnectorDescriptor;
import io.astrasync.connector.api.ConnectorFactory;
import io.astrasync.connector.api.ConnectorOptionDescriptor;
import io.astrasync.connector.api.ConnectorOptionOwner;
import io.astrasync.connector.api.ConnectorOptionType;
import io.astrasync.connector.api.ConnectorRole;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import java.util.Set;

/** Creates strict UTF-8 RFC 4180 file connectors. */
public final class CsvConnectorFactory implements ConnectorFactory {
    public static final String CONNECTOR_NAME = "csv";
    public static final String CONNECTOR_VERSION = "1.1.0";

    private static final ConnectorDescriptor DESCRIPTOR = ConnectorDescriptor.builder(
                    CONNECTOR_NAME,
                    CONNECTOR_VERSION,
                    Set.of(ConnectorRole.SOURCE, ConnectorRole.SINK),
                    Set.of(Capability.BATCH_READ, Capability.BATCH_WRITE))
            .displayName("CSV")
            .option(ConnectorOptionDescriptor.builder(
                            "path",
                            ConnectorOptionType.STRING,
                            ConnectorOptionOwner.JOB,
                            ConnectorRole.SOURCE,
                            ConnectorRole.SINK)
                    .required()
                    .lengthBounds(1, 4_096)
                    .patternKey("local.path")
                    .build())
            .option(ConnectorOptionDescriptor.builder(
                            "nullValue",
                            ConnectorOptionType.STRING,
                            ConnectorOptionOwner.JOB,
                            ConnectorRole.SOURCE,
                            ConnectorRole.SINK)
                    .lengthBounds(0, 1_024)
                    .build())
            .option(ConnectorOptionDescriptor.builder(
                            "malformedRowPolicy",
                            ConnectorOptionType.ENUM,
                            ConnectorOptionOwner.JOB,
                            ConnectorRole.SOURCE)
                    .enumValues("fail")
                    .defaultValue("fail")
                    .build())
            .build();

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
