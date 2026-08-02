package io.astrasync.connector.api.source;

import io.astrasync.connector.api.data.RowBatch;

/** A bounded pull Source whose external resources are owned between open and close. */
public interface BatchSource extends AutoCloseable {
    void open();

    RowBatch readBatch(int maxRows);

    @Override
    void close();
}
