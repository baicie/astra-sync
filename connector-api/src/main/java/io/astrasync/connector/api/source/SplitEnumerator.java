package io.astrasync.connector.api.source;

import java.util.List;

/** Discovers stable, independent full-load splits without opening a split reader. */
@FunctionalInterface
public interface SplitEnumerator {
    List<SourceSplit> enumerate();
}
