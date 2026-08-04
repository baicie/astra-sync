package io.astrasync.connector.api.sink;

/** Exactly-once sink whose logical batch and commit record share one transaction. */
public interface TransactionalBatchSink extends ExactlyOnceBatchSink {}
