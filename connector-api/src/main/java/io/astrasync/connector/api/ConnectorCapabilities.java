package io.astrasync.connector.api;

import java.util.Map;
import java.util.Optional;

public interface ConnectorCapabilities {

    String getName();

    String getVersion();

    Map<Capability, Boolean> getCapabilities();

    Map<String, String> getProperties();

    default boolean hasCapability(Capability capability) {
        return Boolean.TRUE.equals(getCapabilities().get(capability));
    }
}

public enum Capability {
    // Source capabilities
    BATCH_READ,
    STREAM_READ,
    PARALLEL_SNAPSHOT,
    REPLAYABLE_OFFSET,
    SCHEMA_EVOLUTION,
    EXACTLY_ONCE_SOURCE,
    COLUMN_PROJECTION,
    PREDICATE_PUSHDOWN,
    SPLIT_MULTIPLEXING,
    CHANGE_DATA_CAPTURE,

    // Sink capabilities
    APPEND,
    UPSERT,
    DELETE,
    TRANSACTIONAL_COMMIT,
    IDEMPOTENT_WRITE,
    BULK_WRITE,
    PARTITION_WRITE,
    SORTED_WRITE,

    // General capabilities
    SCHEMA_REGISTRATION,
    TRANSFORM_SUPPORT,
    EXACTLY_ONCE,
    AT_LEAST_ONCE,
    AT_MOST_ONCE,
    TRANSACTION_RECOVERY,
    FAULT_TOLERANCE,
}
