package io.astrasync.connector.debezium;

import java.util.Map;
import java.util.Objects;
import java.util.Set;
import org.apache.kafka.connect.runtime.WorkerConfig;
import org.apache.kafka.connect.storage.MemoryOffsetBackingStore;

/** Debezium offset store backed by an AstraSync checkpoint state registration. */
public final class AstraOffsetBackingStore extends MemoryOffsetBackingStore {
    public static final String STATE_ID_PROPERTY = "astrasync.offset.state.id";

    private OffsetStateRegistry.State state;

    @Override
    public void configure(WorkerConfig config) {
        Objects.requireNonNull(config, "config must not be null");
        String stateId = config.originalsStrings().get(STATE_ID_PROPERTY);
        if (stateId == null || stateId.isBlank()) {
            throw new IllegalArgumentException("missing Debezium offset state registration");
        }
        state = OffsetStateRegistry.require(stateId);
        data = state.initialOffsets();
    }

    @Override
    protected void save() {
        state.saved(data);
    }

    @Override
    public Set<Map<String, Object>> connectorPartitions(String connectorName) {
        return Set.of();
    }
}
