package io.astrasync.connector.api.sink;

import io.astrasync.connector.api.data.RowBatch;

/** A batch Sink whose external resources are owned between open and close. */
public interface BatchSink extends AutoCloseable {
    void open();

    void writeBatch(RowBatch batch);

    @Override
    void close();
}
