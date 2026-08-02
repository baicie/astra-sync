package io.astrasync.engine.kernel;

@FunctionalInterface
public interface RecordTransform {
    SyncRecord apply(SyncRecord record);
}
