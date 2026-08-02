package io.astrasync.engine.kernel;

import io.astrasync.connector.api.data.Row;

@FunctionalInterface
public interface RecordTransform {
    Row apply(Row record);
}
