package io.astrasync.connector.debezium;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.source.SplitPosition;
import java.nio.ByteBuffer;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.Map;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import org.apache.kafka.common.config.ConfigDef;
import org.apache.kafka.connect.runtime.WorkerConfig;
import org.junit.jupiter.api.Test;

class AstraOffsetBackingStoreTest {
    @Test
    void persistsMemoryStoreUpdatesIntoRegisteredCheckpointState() throws Exception {
        OffsetStateRegistry.Handle handle =
                OffsetStateRegistry.register("mysql-cdc:v1:store", SplitPosition.unbounded());
        AstraOffsetBackingStore store = new AstraOffsetBackingStore();
        WorkerConfig config =
                new WorkerConfig(new ConfigDef(), Map.of(AstraOffsetBackingStore.STATE_ID_PROPERTY, handle.id()));
        store.configure(config);
        store.start();
        try {
            Map<ByteBuffer, ByteBuffer> offsets = Map.of(buffer("partition"), buffer("offset"));
            AtomicReference<Throwable> callbackFailure = new AtomicReference<>();

            store.set(offsets, (error, ignored) -> callbackFailure.set(error)).get(5, TimeUnit.SECONDS);

            assertThat(callbackFailure.get()).isNull();
            assertThat(store.get(offsets.keySet()).get(5, TimeUnit.SECONDS)).hasSize(1);
            assertThat(handle.revision()).isEqualTo(1);
            assertThat(handle.awaitPositionAfter(0, Duration.ofSeconds(1)).isUnbounded())
                    .isFalse();
            assertThat(store.connectorPartitions("connector")).isEmpty();
        } finally {
            store.stop();
            handle.close();
        }
    }

    @Test
    void rejectsMissingOrUnknownStateRegistrations() {
        AstraOffsetBackingStore store = new AstraOffsetBackingStore();

        assertThatThrownBy(() -> store.configure(null))
                .isInstanceOf(NullPointerException.class)
                .hasMessageContaining("config");
        assertThatThrownBy(() -> store.configure(new WorkerConfig(new ConfigDef(), Map.of())))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("missing Debezium offset state");
        assertThatThrownBy(() -> store.configure(new WorkerConfig(
                        new ConfigDef(), Map.of(AstraOffsetBackingStore.STATE_ID_PROPERTY, "missing"))))
                .isInstanceOf(IllegalStateException.class)
                .hasMessageContaining("not registered");
    }

    private static ByteBuffer buffer(String value) {
        return ByteBuffer.wrap(value.getBytes(StandardCharsets.UTF_8));
    }
}
