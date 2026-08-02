package io.astrasync.engine.kernel;

@FunctionalInterface
public interface RecordSink extends AutoCloseable {
    default void open() {}

    void write(SyncRecord record);

    @Override
    default void close() {}
}
