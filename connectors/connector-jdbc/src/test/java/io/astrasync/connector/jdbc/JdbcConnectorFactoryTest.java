package io.astrasync.connector.jdbc;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.Capability;
import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.ConnectorRole;
import java.util.Map;
import org.junit.jupiter.api.Test;

class JdbcConnectorFactoryTest {
    private final JdbcConnectorFactory factory = new JdbcConnectorFactory();

    @Test
    void exposesBoundedAndOptInReplayableJdbcCapabilities() {
        assertThat(factory.descriptor().name()).isEqualTo("jdbc");
        assertThat(factory.descriptor().version()).isEqualTo("1.1.0");
        assertThat(factory.descriptor().roles()).containsExactlyInAnyOrder(ConnectorRole.SOURCE, ConnectorRole.SINK);
        assertThat(factory.descriptor().capabilities())
                .containsExactlyInAnyOrder(
                        Capability.BATCH_READ,
                        Capability.BATCH_WRITE,
                        Capability.REPLAYABLE_OFFSET,
                        Capability.IDEMPOTENT_WRITE,
                        Capability.UPSERT,
                        Capability.DELETE);
        assertThat(factory.descriptor().connectionRequirement(ConnectorRole.SOURCE))
                .isEqualTo(io.astrasync.connector.api.ConnectionRequirement.REQUIRED);
        assertThat(factory.descriptor().connectionRequirement(ConnectorRole.SINK))
                .isEqualTo(io.astrasync.connector.api.ConnectionRequirement.REQUIRED);
        assertThat(factory.descriptor().options())
                .filteredOn(
                        option -> option.sensitivity() == io.astrasync.connector.api.ConnectorOptionSensitivity.SECRET)
                .extracting(io.astrasync.connector.api.ConnectorOptionDescriptor::key)
                .containsExactly("password", "user");
    }

    @Test
    void validatesOptionsBeforeOpeningAConnectionAndRedactsValues() {
        assertThatThrownBy(() ->
                        factory.createSource(ConnectorConfiguration.of(Map.of("url", "not-jdbc", "query", "SELECT 1"))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("must start with 'jdbc:'");
        assertThatThrownBy(() -> factory.createSink(
                        ConnectorConfiguration.of(Map.of("url", "jdbc:h2:mem:test", "table", "bad-table;drop"))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("unquoted SQL identifier");

        JdbcConnectorOptions options = JdbcConnectorOptions.source(ConnectorConfiguration.of(Map.of(
                "url", "jdbc:h2:mem:secret", "query", "SELECT 'secret-query'", "user", "alice", "password", "secret")));
        assertThat(options.toString())
                .contains("url", "query", "user", "password")
                .doesNotContain("jdbc:h2:mem:secret", "secret-query", "alice", "secret");
    }

    @Test
    void rejectsUnknownAndNonPositiveOptions() {
        assertThatThrownBy(() -> factory.createSource(ConnectorConfiguration.of(
                        Map.of("url", "jdbc:h2:mem:test", "query", "SELECT 1", "unknown", "x"))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("unknown JDBC connector option 'unknown'");
        assertThatThrownBy(() -> factory.createSource(ConnectorConfiguration.of(
                        Map.of("url", "jdbc:h2:mem:test", "query", "SELECT 1", "fetchSize", "0"))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("fetchSize");
    }
}
