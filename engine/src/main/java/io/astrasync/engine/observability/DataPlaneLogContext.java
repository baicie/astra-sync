package io.astrasync.engine.observability;

import java.util.Collections;
import java.util.HashMap;
import java.util.Map;
import org.slf4j.MDC;

/** Scoped structured logging context for Java data-plane execution paths. */
public final class DataPlaneLogContext implements AutoCloseable {
    private static final String TENANT_ID = "tenant_id";
    private static final String JOB_ID = "job_id";
    private static final String EPOCH = "epoch";
    private static final String STAGE = "stage";
    private static final String OUTCOME = "outcome";

    private final Map<String, String> previousValues;
    private boolean closed;

    private DataPlaneLogContext(Map<String, String> values) {
        Map<String, String> previous = new HashMap<>();
        for (Map.Entry<String, String> entry : values.entrySet()) {
            String value = entry.getValue();
            if (value == null || value.isBlank()) {
                continue;
            }
            previous.put(entry.getKey(), MDC.get(entry.getKey()));
            MDC.put(entry.getKey(), value);
        }
        previousValues = Collections.unmodifiableMap(previous);
    }

    /** Opens a task-scoped context with optional execution epoch. */
    public static DataPlaneLogContext open(String tenantId, String jobId, Long executionEpoch) {
        return open(tenantId, jobId, executionEpoch, null, null);
    }

    /** Opens a nested context with optional stage and outcome fields. */
    public static DataPlaneLogContext open(
            String tenantId, String jobId, Long executionEpoch, String stage, String outcome) {
        Map<String, String> values = new HashMap<>();
        values.put(TENANT_ID, tenantId);
        values.put(JOB_ID, jobId);
        if (executionEpoch != null && executionEpoch > 0) {
            values.put(EPOCH, executionEpoch.toString());
        }
        values.put(STAGE, stage);
        values.put(OUTCOME, outcome);
        return new DataPlaneLogContext(values);
    }

    @Override
    public void close() {
        if (closed) {
            return;
        }
        closed = true;
        for (Map.Entry<String, String> entry : previousValues.entrySet()) {
            if (entry.getValue() == null) {
                MDC.remove(entry.getKey());
            } else {
                MDC.put(entry.getKey(), entry.getValue());
            }
        }
    }
}
