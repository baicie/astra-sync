package io.astrasync.connector.debezium;

import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.source.SplitPosition;
import java.time.Duration;
import java.util.Map;
import java.util.Properties;
import org.junit.jupiter.api.Test;

class DebeziumCdcSourceTest {
    @Test
    void validatesConstructionArguments() {
        Properties properties = new Properties();

        assertThatThrownBy(() -> new DebeziumCdcSource(" ", properties, 1, Duration.ofSeconds(1)))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("connectorIdentity");
        assertThatThrownBy(() -> new DebeziumCdcSource("source", null, 1, Duration.ofSeconds(1)))
                .isInstanceOf(NullPointerException.class)
                .hasMessageContaining("connectorProperties");
        assertThatThrownBy(() -> new DebeziumCdcSource("source", properties, 0, Duration.ofSeconds(1)))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("queuedBatches");
        assertThatThrownBy(() -> new DebeziumCdcSource("source", properties, 1, Duration.ZERO))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("commitTimeout");
        assertThatThrownBy(() -> new DebeziumCdcSource("source", properties, 1, Duration.ofSeconds(1), null))
                .isInstanceOf(NullPointerException.class)
                .hasMessageContaining("converter");
    }

    @Test
    void rejectsOperationsOutsideTheOpenLifecycleAndClosesIdempotently() {
        DebeziumCdcSource source =
                new DebeziumCdcSource("mysql-cdc:v1:test", new Properties(), 1, Duration.ofSeconds(1));

        assertThatThrownBy(() -> source.poll(Duration.ofMillis(1)))
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("state is NEW");
        assertThatThrownBy(() -> source.openAt(null))
                .isInstanceOf(NullPointerException.class)
                .hasMessageContaining("resumePosition");
        assertThatThrownBy(() -> source.openAt(new SplitPosition(Map.of("cursor", "1"))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("non-CDC");

        source.close();
        source.close();

        assertThatThrownBy(() -> source.poll(Duration.ofMillis(1)))
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("state is CLOSED");
        assertThatThrownBy(() -> source.openAt(SplitPosition.unbounded()))
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("state is CLOSED");
    }
}
