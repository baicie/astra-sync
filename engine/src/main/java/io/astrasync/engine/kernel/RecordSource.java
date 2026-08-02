package io.astrasync.engine.kernel;

@FunctionalInterface
public interface RecordSource extends AutoCloseable {
    default void open() {}

    SyncBatch readBatch(int maxRecords);

    @Override
    default void close() {}
}
