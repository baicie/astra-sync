package io.astrasync.connector.api.source;

import io.astrasync.connector.api.data.RecordBatch;

public interface PollResult<T> {

    boolean hasMoreData();

    boolean isBarrier();

    boolean isWatermark();

    boolean isEndOfSplit();

    T getBatch();

    long getCheckpointId();

    Watermark getWatermark();
}
