package io.astrasync.connector.api;

public interface Watermark {

    long getTimestamp();

    String getWatermarkStrategy();

    String getSourceTable();

    int getParallelism();

    long getEventTime();
}
