package io.astrasync.connector.postgres.cdc;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.Capability;
import io.astrasync.connector.api.ConnectorConfiguration;
import java.util.HashMap;
import java.util.Map;
import org.junit.jupiter.api.Test;

class PostgresCdcConnectorFactoryTest {
    @Test
    void exposesNativeReplayableCdcAndBuildsLogicalReplicationProperties() {
        PostgresCdcConnectorFactory factory = new PostgresCdcConnectorFactory();
        PostgresCdcConnectorOptions options = PostgresCdcConnectorOptions.from(configuration(Map.of()));

        assertThat(factory.descriptor().capabilities())
                .contains(
                        Capability.STREAM_READ,
                        Capability.CHANGE_DATA_CAPTURE,
                        Capability.REPLAYABLE_OFFSET,
                        Capability.EXACTLY_ONCE_SOURCE);
        assertThat(factory.descriptor().version()).isEqualTo("1.1.0");
        assertThat(factory.descriptor().connectionRequirement(io.astrasync.connector.api.ConnectorRole.SOURCE))
                .isEqualTo(io.astrasync.connector.api.ConnectionRequirement.REQUIRED);
        assertThat(factory.descriptor().options())
                .filteredOn(
                        option -> option.sensitivity() == io.astrasync.connector.api.ConnectorOptionSensitivity.SECRET)
                .extracting(io.astrasync.connector.api.ConnectorOptionDescriptor::key)
                .containsExactly("password", "username");
        assertThat(factory.descriptor().optionPrefixes()).isEmpty();
        assertThat(factory.createCdcSource(configuration(Map.of())))
                .isInstanceOf(io.astrasync.connector.debezium.DebeziumCdcSource.class);
        assertThat(options.properties())
                .containsEntry("connector.class", "io.debezium.connector.postgresql.PostgresConnector")
                .containsEntry("plugin.name", "pgoutput")
                .containsEntry("slot.name", "astrasync_shop")
                .containsEntry("publication.name", "astrasync_shop_publication")
                .containsEntry("publication.autocreate.mode", "filtered");
        assertThat(options.identity()).startsWith("postgres-cdc:v1:");
        assertThat(options.toString()).doesNotContain("change-me", "db-user", "localhost");
    }

    @Test
    void rejectsInvalidReplicationIdentifiersAndProtectedOverrides() {
        assertThatThrownBy(() -> PostgresCdcConnectorOptions.from(configuration(Map.of("slotName", "Bad-Slot"))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("lowercase PostgreSQL identifier");
        assertThatThrownBy(() ->
                        PostgresCdcConnectorOptions.from(configuration(Map.of("debezium.slot.name", "other_slot"))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("cannot be overridden");
        assertThatThrownBy(() -> PostgresCdcConnectorOptions.from(configuration(Map.of("unknown", "value"))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("unknown PostgreSQL CDC connector option");
    }

    private static ConnectorConfiguration configuration(Map<String, String> overrides) {
        Map<String, String> values = new HashMap<>(Map.of(
                "hostname", "localhost",
                "username", "db-user",
                "password", "change-me",
                "database", "shop",
                "schemas", "public",
                "tables", "public.orders",
                "topicPrefix", "shop-source",
                "slotName", "astrasync_shop",
                "publicationName", "astrasync_shop_publication"));
        values.putAll(overrides);
        return ConnectorConfiguration.of(values);
    }
}
