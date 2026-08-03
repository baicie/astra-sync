package io.astrasync.connector.api.source;

/** Enumerates source splits and materializes a bounded reader for one split. */
public interface SplitSource extends SplitEnumerator {
    BatchSource createSource(SourceSplit split);
}
