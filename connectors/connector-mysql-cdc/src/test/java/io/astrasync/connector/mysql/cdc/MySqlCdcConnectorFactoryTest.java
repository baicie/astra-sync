package io.astrasync.connector.mysql.cdc;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.Capability;
import io.astrasync.connector.api.ConnectorConfiguration;
import java.nio.file.Path;
import java.util.HashMap;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class MySqlCdcConnectorFactoryTest {
    @TempDir
    Path temporaryDirectory;

    @Test
    void exposesNativeReplayableCdcAndBuildsDebeziumProperties() {
        MySqlCdcConnectorFactory factory = new MySqlCdcConnectorFactory();
        MySqlCdcConnectorOptions options = MySqlCdcConnectorOptions.from(configuration(Map.of()));

        assertThat(factory.descriptor().capabilities())
                .contains(
                        Capability.STREAM_READ,
                        Capability.CHANGE_DATA_CAPTURE,
                        Capability.REPLAYABLE_OFFSET,
                        Capability.EXACTLY_ONCE_SOURCE);
        assertThat(factory.createCdcSource(configuration(Map.of()))).isInstanceOf(MySqlCdcSource.class);
        assertThat(options.properties())
                .containsEntry("connector.class", "io.debezium.connector.mysql.MySqlConnector")
                .containsEntry("database.include.list", "shop")
                .containsEntry("table.include.list", "shop.orders")
                .containsEntry("snapshot.mode", "initial");
        assertThat(options.identity()).startsWith("mysql-cdc:v1:");
        assertThat(options.toString()).doesNotContain("change-me", "db-user", "localhost");
    }

    @Test
    void validatesQueueIdentityAndProtectedProperties() {
        assertThatThrownBy(() -> MySqlCdcConnectorOptions.from(configuration(Map.of("maxQueueSize", "100"))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("greater than maxBatchSize");
        assertThatThrownBy(() ->
                        MySqlCdcConnectorOptions.from(configuration(Map.of("debezium.offset.storage", "other.Store"))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("cannot be overridden");
        assertThatThrownBy(() -> MySqlCdcConnectorOptions.from(configuration(Map.of("typo", "value"))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("unknown MySQL CDC connector option");
    }

    private ConnectorConfiguration configuration(Map<String, String> overrides) {
        Map<String, String> values = new HashMap<>(Map.of(
                "hostname", "localhost",
                "username", "db-user",
                "password", "change-me",
                "database", "shop",
                "tables", "shop.orders",
                "topicPrefix", "shop-source",
                "serverId", "5401",
                "schemaHistoryFile",
                        temporaryDirectory.resolve("mysql-history.dat").toString()));
        values.putAll(overrides);
        return ConnectorConfiguration.of(values);
    }
}
