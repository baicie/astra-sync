package io.astrasync.connector.jdbc;

import io.astrasync.connector.api.Capability;
import io.astrasync.connector.api.ConnectionRequirement;
import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.ConnectorDescriptor;
import io.astrasync.connector.api.ConnectorFactory;
import io.astrasync.connector.api.ConnectorOptionDescriptor;
import io.astrasync.connector.api.ConnectorOptionOwner;
import io.astrasync.connector.api.ConnectorOptionSensitivity;
import io.astrasync.connector.api.ConnectorOptionType;
import io.astrasync.connector.api.ConnectorRole;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.sink.CdcSink;
import io.astrasync.connector.api.source.BatchSource;
import java.util.Set;

/** Creates generic JDBC Source and Sink instances. */
public final class JdbcConnectorFactory implements ConnectorFactory {
    public static final String CONNECTOR_NAME = "jdbc";
    public static final String CONNECTOR_VERSION = "1.1.0";

    private static final ConnectorDescriptor DESCRIPTOR = buildDescriptor();

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

    @Override
    public CdcSink createCdcSink(ConnectorConfiguration configuration) {
        return new JdbcCdcSink(JdbcCdcSinkOptions.from(configuration));
    }

    private static ConnectorDescriptor buildDescriptor() {
        ConnectorDescriptor.Builder descriptor = ConnectorDescriptor.builder(
                        CONNECTOR_NAME,
                        CONNECTOR_VERSION,
                        Set.of(ConnectorRole.SOURCE, ConnectorRole.SINK),
                        Set.of(
                                Capability.BATCH_READ,
                                Capability.BATCH_WRITE,
                                Capability.REPLAYABLE_OFFSET,
                                Capability.IDEMPOTENT_WRITE,
                                Capability.UPSERT,
                                Capability.DELETE))
                .displayName("JDBC")
                .connectionRequirement(ConnectorRole.SOURCE, ConnectionRequirement.REQUIRED)
                .connectionRequirement(ConnectorRole.SINK, ConnectionRequirement.REQUIRED);

        descriptor.option(connectionString("url", ConnectorOptionSensitivity.RESTRICTED, true));
        descriptor.option(connectionString("user", ConnectorOptionSensitivity.SECRET, false));
        descriptor.option(connectionString("password", ConnectorOptionSensitivity.SECRET, false));
        descriptor.option(jobString("query", true, ConnectorRole.SOURCE)
                .lengthBounds(1, 65_536)
                .build());
        descriptor.option(jobString("table", false, ConnectorRole.SOURCE, ConnectorRole.SINK)
                .patternKey("sql.table")
                .build());
        descriptor.option(jobString("commitTokenTable", false, ConnectorRole.SINK)
                .patternKey("sql.table")
                .advanced()
                .build());
        descriptor.option(jobString("resumeColumn", false, ConnectorRole.SOURCE)
                .patternKey("sql.column")
                .build());
        descriptor.option(
                jobString("resumeValue", false, ConnectorRole.SOURCE).advanced().build());
        descriptor.option(jobInteger("fetchSize", 1, Integer.MAX_VALUE, ConnectorRole.SOURCE));
        descriptor.option(
                jobInteger("queryTimeoutSeconds", 0, Integer.MAX_VALUE, ConnectorRole.SOURCE, ConnectorRole.SINK));
        descriptor.option(jobString("splitColumn", false, ConnectorRole.SOURCE)
                .patternKey("sql.column")
                .build());
        descriptor.option(jobInteger("splitCount", 1, Integer.MAX_VALUE, ConnectorRole.SOURCE));
        descriptor.option(jobString("keyColumns", false, ConnectorRole.SINK)
                .patternKey("sql.column-list")
                .build());
        descriptor.option(ConnectorOptionDescriptor.builder(
                        "allowTruncate", ConnectorOptionType.BOOLEAN, ConnectorOptionOwner.JOB, ConnectorRole.SINK)
                .defaultValue("false")
                .advanced()
                .build());
        return descriptor.build();
    }

    private static ConnectorOptionDescriptor connectionString(
            String key, ConnectorOptionSensitivity sensitivity, boolean required) {
        ConnectorOptionDescriptor.Builder option = ConnectorOptionDescriptor.builder(
                        key,
                        ConnectorOptionType.STRING,
                        ConnectorOptionOwner.CONNECTION,
                        ConnectorRole.SOURCE,
                        ConnectorRole.SINK)
                .sensitivity(sensitivity)
                .lengthBounds(1, 4_096);
        if (required) {
            option.required();
        }
        if ("url".equals(key)) {
            option.patternKey("jdbc.url");
        }
        return option.build();
    }

    private static ConnectorOptionDescriptor.Builder jobString(String key, boolean required, ConnectorRole... roles) {
        ConnectorOptionDescriptor.Builder option = ConnectorOptionDescriptor.builder(
                        key, ConnectorOptionType.STRING, ConnectorOptionOwner.JOB, roles)
                .lengthBounds(1, 65_536);
        return required ? option.required() : option;
    }

    private static ConnectorOptionDescriptor jobInteger(
            String key, long minimum, long maximum, ConnectorRole... roles) {
        return ConnectorOptionDescriptor.builder(key, ConnectorOptionType.INTEGER, ConnectorOptionOwner.JOB, roles)
                .numericBounds(minimum, maximum)
                .build();
    }
}
