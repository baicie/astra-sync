package io.astrasync.engine.runtime;

import io.astrasync.connector.api.sink.CdcSink;
import io.astrasync.connector.api.source.CdcSource;
import java.time.Duration;
import java.util.Objects;

/** One unbounded CDC source-to-sink task with a bounded poll interval. */
public record CdcTask(String taskId, CdcSource source, CdcSink sink, Duration pollTimeout) {
    public CdcTask {
        taskId = requireText(taskId, "taskId");
        source = Objects.requireNonNull(source, "source must not be null");
        sink = Objects.requireNonNull(sink, "sink must not be null");
        pollTimeout = Objects.requireNonNull(pollTimeout, "pollTimeout must not be null");
        if (pollTimeout.isZero() || pollTimeout.isNegative()) {
            throw new IllegalArgumentException("pollTimeout must be positive");
        }
    }

    private static String requireText(String value, String name) {
        Objects.requireNonNull(value, name + " must not be null");
        if (value.isBlank()) {
            throw new IllegalArgumentException(name + " must not be blank");
        }
        return value;
    }
}
