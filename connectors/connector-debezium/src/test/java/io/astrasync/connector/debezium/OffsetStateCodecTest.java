package io.astrasync.connector.debezium;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.source.SplitPosition;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.util.Map;
import org.junit.jupiter.api.Test;

class OffsetStateCodecTest {
    @Test
    void roundTripsOpaqueKafkaConnectOffsets() {
        Map<ByteBuffer, ByteBuffer> offsets =
                Map.of(buffer("partition-a"), buffer("offset-a"), buffer("partition-b"), buffer("offset-b"));

        SplitPosition position = OffsetStateCodec.encode("mysql-cdc:v1:test", offsets);
        Map<ByteBuffer, ByteBuffer> restored = OffsetStateCodec.decode("mysql-cdc:v1:test", position);

        assertThat(restored).hasSize(2);
        assertThat(text(restored.get(buffer("partition-a")))).isEqualTo("offset-a");
        assertThat(text(restored.get(buffer("partition-b")))).isEqualTo("offset-b");
    }

    @Test
    void rejectsPositionsFromAnotherConnectorOrFormat() {
        SplitPosition position =
                OffsetStateCodec.encode("mysql-cdc:v1:first", Map.of(buffer("partition"), buffer("offset")));

        assertThatThrownBy(() -> OffsetStateCodec.decode("mysql-cdc:v1:second", position))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("different connector");
        assertThatThrownBy(
                        () -> OffsetStateCodec.decode("mysql-cdc:v1:first", new SplitPosition(Map.of("resume", "10"))))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("non-CDC");
    }

    private static ByteBuffer buffer(String value) {
        return ByteBuffer.wrap(value.getBytes(StandardCharsets.UTF_8));
    }

    private static String text(ByteBuffer value) {
        ByteBuffer copy = value.duplicate();
        byte[] bytes = new byte[copy.remaining()];
        copy.get(bytes);
        return new String(bytes, StandardCharsets.UTF_8);
    }
}
