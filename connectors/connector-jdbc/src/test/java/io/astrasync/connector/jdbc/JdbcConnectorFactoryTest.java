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
    void exposesOnlyTheBoundedJdbcContract() {
        assertThat(factory.descriptor().name()).isEqualTo("jdbc");
        assertThat(factory.descriptor().version()).isEqualTo("1.0.0");
        assertThat(factory.descriptor().roles()).containsExactlyInAnyOrder(ConnectorRole.SOURCE, ConnectorRole.SINK);
        assertThat(factory.descriptor().capabilities())
                .containsExactlyInAnyOrder(Capability.BATCH_READ, Capability.BATCH_WRITE);
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
