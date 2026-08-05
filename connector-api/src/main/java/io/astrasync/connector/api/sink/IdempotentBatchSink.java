package io.astrasync.connector.api.sink;

/** Exactly-once sink backed by a durable idempotency key or marker. */
public interface IdempotentBatchSink extends ExactlyOnceBatchSink {}
