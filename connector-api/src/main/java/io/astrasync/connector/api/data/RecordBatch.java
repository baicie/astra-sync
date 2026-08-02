package io.astrasync.connector.api.data;

import org.apache.arrow.memory.BufferAllocator;
import org.apache.arrow.vector.VectorSchemaRoot;
import org.apache.arrow.vector.types.pojo.Schema;

public interface RecordBatch {

    Schema getSchema();

    VectorSchemaRoot getVectorSchemaRoot();

    BufferAllocator getAllocator();

    int getRowCount();

    long getByteCount();

    RecordBatch slice(int start, int length);

    RecordBatch compact();

    void retain();

    void release();
}
