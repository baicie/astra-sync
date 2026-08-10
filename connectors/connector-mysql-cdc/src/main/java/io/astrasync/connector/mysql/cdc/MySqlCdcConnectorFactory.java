package io.astrasync.connector.mysql.cdc;

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
import io.astrasync.connector.api.source.CdcSource;
import java.util.Set;

/** Creates native MySQL binlog sources backed by Debezium. */
public final class MySqlCdcConnectorFactory implements ConnectorFactory {
    public static final String CONNECTOR_NAME = "mysql-cdc";
    public static final String CONNECTOR_VERSION = "1.1.0";

    private static final ConnectorDescriptor DESCRIPTOR = buildDescriptor();

    @Override
    public ConnectorDescriptor descriptor() {
        return DESCRIPTOR;
    }

    @Override
    public CdcSource createCdcSource(ConnectorConfiguration configuration) {
        return new MySqlCdcSource(MySqlCdcConnectorOptions.from(configuration));
    }

    private static ConnectorDescriptor buildDescriptor() {
        ConnectorDescriptor.Builder descriptor = ConnectorDescriptor.builder(
                        CONNECTOR_NAME,
                        CONNECTOR_VERSION,
                        Set.of(ConnectorRole.SOURCE),
                        Set.of(
                                Capability.STREAM_READ,
                                Capability.REPLAYABLE_OFFSET,
                                Capability.EXACTLY_ONCE_SOURCE,
                                Capability.CHANGE_DATA_CAPTURE,
                                Capability.SCHEMA_EVOLUTION,
                                Capability.FAULT_TOLERANCE))
                .displayName("MySQL CDC")
                .connectionRequirement(ConnectorRole.SOURCE, ConnectionRequirement.REQUIRED);

        descriptor.option(connectionString("hostname", ConnectorOptionSensitivity.RESTRICTED, true));
        descriptor.option(connectionInteger("port", 3306, 1, 65_535));
        descriptor.option(connectionString("username", ConnectorOptionSensitivity.SECRET, true));
        descriptor.option(connectionString("password", ConnectorOptionSensitivity.SECRET, true));
        descriptor.option(connectionString("database", ConnectorOptionSensitivity.RESTRICTED, true));
        descriptor.option(connectionString("sslMode", ConnectorOptionSensitivity.PUBLIC, false));
        descriptor.option(jobString("tables", true).build());
        descriptor.option(jobString("topicPrefix", true)
                .patternKey("debezium.topic-prefix")
                .build());
        descriptor.option(jobInteger("serverId", null, 1, 4_294_967_295L, true));
        descriptor.option(ConnectorOptionDescriptor.builder(
                        "snapshotMode", ConnectorOptionType.ENUM, ConnectorOptionOwner.JOB, ConnectorRole.SOURCE)
                .enumValues("initial", "when_needed", "never")
                .defaultValue("initial")
                .build());
        descriptor.option(jobString("schemaHistoryFile", true)
                .patternKey("local.path")
                .advanced()
                .build());
        descriptor.option(jobInteger("maxBatchSize", "2048", 1, Integer.MAX_VALUE, false));
        descriptor.option(jobInteger("maxQueueSize", "8192", 2, Integer.MAX_VALUE, false));
        descriptor.option(jobInteger("pollIntervalMillis", "500", 1, 60_000, false));
        descriptor.option(jobInteger("heartbeatIntervalMillis", "10000", 0, 3_600_000, false));
        descriptor.option(jobInteger("queuedBatches", "1", 1, 1_024, false));
        descriptor.option(jobInteger("offsetCommitTimeoutMillis", "30000", 1, 600_000, false));
        return descriptor.build();
    }

    private static ConnectorOptionDescriptor connectionString(
            String key, ConnectorOptionSensitivity sensitivity, boolean required) {
        ConnectorOptionDescriptor.Builder option = ConnectorOptionDescriptor.builder(
                        key, ConnectorOptionType.STRING, ConnectorOptionOwner.CONNECTION, ConnectorRole.SOURCE)
                .sensitivity(sensitivity)
                .lengthBounds(1, 4_096);
        return (required ? option.required() : option).build();
    }

    private static ConnectorOptionDescriptor connectionInteger(
            String key, long defaultValue, long minimum, long maximum) {
        return ConnectorOptionDescriptor.builder(
                        key, ConnectorOptionType.INTEGER, ConnectorOptionOwner.CONNECTION, ConnectorRole.SOURCE)
                .numericBounds(minimum, maximum)
                .defaultValue(Long.toString(defaultValue))
                .build();
    }

    private static ConnectorOptionDescriptor.Builder jobString(String key, boolean required) {
        ConnectorOptionDescriptor.Builder option = ConnectorOptionDescriptor.builder(
                        key, ConnectorOptionType.STRING, ConnectorOptionOwner.JOB, ConnectorRole.SOURCE)
                .lengthBounds(1, 65_536);
        return required ? option.required() : option;
    }

    private static ConnectorOptionDescriptor jobInteger(
            String key, String defaultValue, long minimum, long maximum, boolean required) {
        ConnectorOptionDescriptor.Builder option = ConnectorOptionDescriptor.builder(
                        key, ConnectorOptionType.INTEGER, ConnectorOptionOwner.JOB, ConnectorRole.SOURCE)
                .numericBounds(minimum, maximum);
        if (defaultValue != null) {
            option.defaultValue(defaultValue);
        }
        if (required) {
            option.required();
        }
        return option.build();
    }
}
