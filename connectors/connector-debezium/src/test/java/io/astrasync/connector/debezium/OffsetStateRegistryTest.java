package io.astrasync.connector.debezium;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.source.SplitPosition;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.Map;
import org.junit.jupiter.api.Test;

class OffsetStateRegistryTest {
    @Test
    void savesOffsetsAndReturnsDefensiveCheckpointCopies() {
        OffsetStateRegistry.Handle handle =
                OffsetStateRegistry.register("mysql-cdc:v1:test", SplitPosition.unbounded());
        OffsetStateRegistry.State state = OffsetStateRegistry.require(handle.id());
        try {
            assertThat(handle.revision()).isZero();
            assertThat(state.initialOffsets()).isEmpty();

            state.saved(Map.of(buffer("partition"), buffer("offset-1")));

            assertThat(handle.revision()).isEqualTo(1);
            SplitPosition position = handle.awaitPositionAfter(0, Duration.ofSeconds(1));
            assertThat(text(OffsetStateCodec.decode("mysql-cdc:v1:test", position)
                            .get(buffer("partition"))))
                    .isEqualTo("offset-1");

            Map<ByteBuffer, ByteBuffer> copy = state.initialOffsets();
            copy.clear();
            assertThat(state.initialOffsets()).hasSize(1);
        } finally {
            handle.close();
        }

        assertThatThrownBy(() -> OffsetStateRegistry.require(handle.id()))
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("not registered");
        handle.close();
        state.saved(Map.of(buffer("partition"), buffer("offset-2")));
        assertThat(state.revision()).isEqualTo(1);
        assertThatThrownBy(() -> handle.awaitPositionAfter(1, Duration.ofMillis(10)))
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("closed");
    }

    @Test
    void validatesTimeoutAndPreservesInterruptStatus() {
        OffsetStateRegistry.Handle handle =
                OffsetStateRegistry.register("postgres-cdc:v1:test", SplitPosition.unbounded());
        try {
            assertThatThrownBy(() -> handle.awaitPositionAfter(0, Duration.ZERO))
                    .isInstanceOf(IllegalArgumentException.class)
                    .hasMessageContaining("positive");
            assertThatThrownBy(() -> handle.awaitPositionAfter(0, null))
                    .isInstanceOf(NullPointerException.class)
                    .hasMessageContaining("timeout");
            assertThatThrownBy(() -> handle.awaitPositionAfter(0, Duration.ofMillis(1)))
                    .isInstanceOf(IllegalStateException.class)
                    .hasMessageContaining("timed out");

            Thread.currentThread().interrupt();
            assertThatThrownBy(() -> handle.awaitPositionAfter(0, Duration.ofSeconds(1)))
                    .isInstanceOf(IllegalStateException.class)
                    .hasMessageContaining("interrupted");
            assertThat(Thread.currentThread().isInterrupted()).isTrue();
        } finally {
            Thread.interrupted();
            handle.close();
        }
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
