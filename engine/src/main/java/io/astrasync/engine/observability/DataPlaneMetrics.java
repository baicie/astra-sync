package io.astrasync.engine.observability;

import io.micrometer.core.instrument.Counter;
import io.micrometer.core.instrument.DistributionSummary;
import io.micrometer.core.instrument.MeterRegistry;
import io.micrometer.core.instrument.Timer;
import io.micrometer.prometheusmetrics.PrometheusConfig;
import io.micrometer.prometheusmetrics.PrometheusMeterRegistry;
import java.util.Objects;
import java.util.Set;
import java.util.concurrent.TimeUnit;
import java.util.regex.Pattern;

/** Records bounded Prometheus-compatible observations for the Java data plane. */
public final class DataPlaneMetrics {
    public static final String UNKNOWN_TENANT_ID = "_unknown";
    public static final String UNKNOWN_JOB_ID = "_unknown";

    private static final Pattern CANONICAL_UUID =
            Pattern.compile("[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}");
    private static final Set<String> REJECTION_REASONS = Set.of("SINK_OPEN", "SINK_WRITE", "SINK_CLOSE");
    private static final Set<String> CHECKPOINT_OUTCOMES = Set.of("success", "failure");
    private static final PrometheusMeterRegistry PROCESS_REGISTRY =
            new PrometheusMeterRegistry(PrometheusConfig.DEFAULT);
    private static final DataPlaneMetrics GLOBAL = new DataPlaneMetrics(PROCESS_REGISTRY);

    private final MeterRegistry registry;

    /** Creates a recorder backed by the supplied registry. */
    public DataPlaneMetrics(MeterRegistry registry) {
        this.registry = Objects.requireNonNull(registry, "registry must not be null");
    }

    /** Returns the process-local recorder used by Coordinator and Worker runtime paths. */
    public static DataPlaneMetrics global() {
        return GLOBAL;
    }

    /** Returns the process-local Prometheus registry used by the optional HTTP endpoint. */
    public static PrometheusMeterRegistry processRegistry() {
        return PROCESS_REGISTRY;
    }

    /** Records source records accepted by a Worker batch. */
    public void recordRecordsRead(String jobId, long count) {
        increment("worker.records.read", jobId, count);
    }

    /** Records records committed to a Worker sink batch. */
    public void recordRecordsWritten(String jobId, long count) {
        increment("worker.records.written", jobId, count);
    }

    /** Records sink records rejected with a stable reason code. */
    public void recordRecordsRejected(String jobId, String reason, long count) {
        if (count <= 0) {
            return;
        }
        String stableReason = REJECTION_REASONS.contains(reason) ? reason : "UNKNOWN";
        Counter.builder("worker.records.rejected")
                .tags(commonTags(jobId))
                .tag("reason", stableReason)
                .register(registry)
                .increment(count);
    }

    /** Records the configured batch size selected by the Coordinator. */
    public void recordBatchSize(String jobId, int batchSize) {
        if (batchSize <= 0) {
            return;
        }
        DistributionSummary.builder("coordinator.batch.size")
                .baseUnit("records")
                .tags(commonTags(jobId))
                .register(registry)
                .record(batchSize);
    }

    /** Records a completed Coordinator batch duration under an allowlisted stage. */
    public void recordBatchDuration(String jobId, String stage, long durationNanos) {
        recordTimer(
                "coordinator.batch.duration", jobId, durationNanos, "stage", "read".equals(stage) ? "read" : "unknown");
    }

    /** Records durable checkpoint latency with a bounded outcome. */
    public void recordCheckpointDuration(String jobId, String outcome, long durationNanos) {
        String stableOutcome = CHECKPOINT_OUTCOMES.contains(outcome) ? outcome : "failure";
        recordTimer("coordinator.checkpoint.duration", jobId, durationNanos, "outcome", stableOutcome);
    }

    /** Records successfully enqueued Worker-local spill payload bytes. */
    public void recordSpillBytes(String jobId, long bytes) {
        increment("coordinator.spill.bytes", jobId, bytes);
    }

    private void increment(String name, String jobId, long count) {
        if (count <= 0) {
            return;
        }
        Counter.builder(name).tags(commonTags(jobId)).register(registry).increment(count);
    }

    private void recordTimer(String name, String jobId, long durationNanos, String label, String value) {
        if (durationNanos < 0) {
            return;
        }
        Timer.builder(name)
                .tags(commonTags(jobId))
                .tag(label, value)
                .register(registry)
                .record(durationNanos, TimeUnit.NANOSECONDS);
    }

    private static String[] commonTags(String jobId) {
        return new String[] {"tenant_id", UNKNOWN_TENANT_ID, "job_id", canonicalJobId(jobId)};
    }

    private static String canonicalJobId(String jobId) {
        return jobId != null && CANONICAL_UUID.matcher(jobId).matches() ? jobId : UNKNOWN_JOB_ID;
    }
}
