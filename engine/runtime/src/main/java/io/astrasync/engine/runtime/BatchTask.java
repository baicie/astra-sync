package io.astrasync.engine.runtime;

import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.connector.api.source.SourceSplit;
import java.util.Objects;

/** A self-contained batch split assigned to one Worker. */
public record BatchTask(
        SourceSplit split,
        BatchSource source,
        BatchSink sink,
        int maxBatchRecords,
        int maxInFlightBatches,
        boolean exactlyOnce,
        AdaptiveBatchPolicy batchPolicy,
        SpillPolicy spillPolicy,
        String jobId,
        String tenantId,
        String requestId) {
    public static final String UNKNOWN_JOB_ID = "_unknown";
    public static final String UNKNOWN_TENANT_ID = "_unknown";
    public static final String UNKNOWN_REQUEST_ID = "_unknown";

    public BatchTask(
            SourceSplit split,
            BatchSource source,
            BatchSink sink,
            int maxBatchRecords,
            int maxInFlightBatches,
            boolean exactlyOnce,
            AdaptiveBatchPolicy batchPolicy,
            SpillPolicy spillPolicy,
            String jobId,
            String tenantId) {
        this(
                split,
                source,
                sink,
                maxBatchRecords,
                maxInFlightBatches,
                exactlyOnce,
                batchPolicy,
                spillPolicy,
                jobId,
                tenantId,
                UNKNOWN_REQUEST_ID);
    }

    public BatchTask(
            SourceSplit split,
            BatchSource source,
            BatchSink sink,
            int maxBatchRecords,
            int maxInFlightBatches,
            boolean exactlyOnce,
            AdaptiveBatchPolicy batchPolicy,
            SpillPolicy spillPolicy) {
        this(
                split,
                source,
                sink,
                maxBatchRecords,
                maxInFlightBatches,
                exactlyOnce,
                batchPolicy,
                spillPolicy,
                UNKNOWN_JOB_ID,
                UNKNOWN_TENANT_ID,
                UNKNOWN_REQUEST_ID);
    }

    public BatchTask(
            SourceSplit split, BatchSource source, BatchSink sink, int maxBatchRecords, int maxInFlightBatches) {
        this(split, source, sink, maxBatchRecords, maxInFlightBatches, false);
    }

    public BatchTask(
            SourceSplit split,
            BatchSource source,
            BatchSink sink,
            int maxBatchRecords,
            int maxInFlightBatches,
            boolean exactlyOnce) {
        this(
                split,
                source,
                sink,
                maxBatchRecords,
                maxInFlightBatches,
                exactlyOnce,
                AdaptiveBatchPolicy.fixed(maxBatchRecords),
                SpillPolicy.disabled());
    }

    public BatchTask(
            SourceSplit split,
            BatchSource source,
            BatchSink sink,
            int maxBatchRecords,
            int maxInFlightBatches,
            boolean exactlyOnce,
            AdaptiveBatchPolicy batchPolicy) {
        this(
                split,
                source,
                sink,
                maxBatchRecords,
                maxInFlightBatches,
                exactlyOnce,
                batchPolicy,
                SpillPolicy.disabled());
    }

    public BatchTask {
        split = Objects.requireNonNull(split, "split must not be null");
        source = Objects.requireNonNull(source, "source must not be null");
        sink = Objects.requireNonNull(sink, "sink must not be null");
        if (maxBatchRecords <= 0) {
            throw new IllegalArgumentException("maxBatchRecords must be positive");
        }
        if (maxInFlightBatches <= 0) {
            throw new IllegalArgumentException("maxInFlightBatches must be positive");
        }
        batchPolicy = Objects.requireNonNull(batchPolicy, "batchPolicy must not be null");
        spillPolicy = Objects.requireNonNull(spillPolicy, "spillPolicy must not be null");
        if (batchPolicy.minBatchRecords() > maxBatchRecords || batchPolicy.initialBatchRecords() > maxBatchRecords) {
            throw new IllegalArgumentException("batch policy bounds must not exceed maxBatchRecords");
        }
        jobId = normalizeIdentity(jobId, UNKNOWN_JOB_ID);
        tenantId = normalizeIdentity(tenantId, UNKNOWN_TENANT_ID);
        requestId = normalizeIdentity(requestId, UNKNOWN_REQUEST_ID);
    }

    public String taskId() {
        return split.splitId();
    }

    /** Returns a copy with the trusted execution identity applied without changing task resources. */
    public BatchTask withIdentity(String jobId, String tenantId) {
        return withIdentity(jobId, tenantId, UNKNOWN_REQUEST_ID);
    }

    /** Returns a copy with the trusted execution and request identity applied. */
    public BatchTask withIdentity(String jobId, String tenantId, String requestId) {
        return new BatchTask(
                split,
                source,
                sink,
                maxBatchRecords,
                maxInFlightBatches,
                exactlyOnce,
                batchPolicy,
                spillPolicy,
                jobId,
                tenantId,
                requestId);
    }

    private static String normalizeIdentity(String value, String fallback) {
        return value == null || value.isBlank() ? fallback : value;
    }
}
