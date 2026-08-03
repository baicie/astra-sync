package io.astrasync.engine.runtime;

import io.astrasync.connector.api.source.SplitEnumerator;

/** Runtime alias for the connector split enumeration contract. */
@FunctionalInterface
public interface BatchSplitEnumerator extends SplitEnumerator {}
