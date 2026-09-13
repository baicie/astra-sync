package io.astrasync.engine.observability;

import io.micrometer.core.instrument.Counter;
import io.micrometer.core.instrument.DistributionSummary;
import io.micrometer.core.instrument.MeterRegistry;
import io.micrometer.core.instrument.Timer;
import io.micrometer.prometheusmetrics.PrometheusConfig;
import io.micrometer.prometheusmetrics.PrometheusMeterRegistry;
import io.prometheus.metrics.core.datapoints.CounterDataPoint;
import io.prometheus.metrics.core.datapoints.DistributionDataPoint;
import io.prometheus.metrics.model.registry.PrometheusRegistry;
import io.prometheus.metrics.model.snapshots.Labels;
import io.prometheus.metrics.model.snapshots.Unit;
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
    private static final Set<String> BATCH_STAGES = Set.of("read", "write");
    private static final Set<String> CHECKPOINT_OUTCOMES = Set.of("success", "failure");
    private static final PrometheusMeterRegistry PROCESS_REGISTRY =
            new PrometheusMeterRegistry(PrometheusConfig.DEFAULT);
    private static final DataPlaneMetrics GLOBAL = new DataPlaneMetrics(PROCESS_REGISTRY);
    private static final Unit RECORDS = new Unit("records");

    private final MeterRegistry registry;
    private final PrometheusBackend prometheus;

    /** Creates a recorder backed by the supplied registry. */
    public DataPlaneMetrics(MeterRegistry registry) {
        this.registry = Objects.requireNonNull(registry, "registry must not be null");
        this.prometheus = registry instanceof PrometheusMeterRegistry prometheusRegistry
                ? new PrometheusBackend(prometheusRegistry.getPrometheusRegistry())
                : null;
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
        recordRecordsRead(UNKNOWN_TENANT_ID, jobId, count);
    }

    /** Records source records accepted by a Worker batch under a trusted tenant identity. */
    public void recordRecordsRead(String tenantId, String jobId, long count) {
        recordRecordsRead(tenantId, jobId, null, count);
    }

    /** Records source records with trusted identity and an optional request-id exemplar. */
    public void recordRecordsRead(String tenantId, String jobId, String requestId, long count) {
        if (count <= 0) {
            return;
        }
        Labels exemplar = requestExemplar(requestId);
        if (prometheus != null) {
            increment(
                    prometheus.recordsRead.labelValues(canonicalTenantId(tenantId), canonicalJobId(jobId)),
                    count,
                    exemplar);
            return;
        }
        increment("worker.records.read", tenantId, jobId, count);
    }

    /** Records records committed to a Worker sink batch. */
    public void recordRecordsWritten(String jobId, long count) {
        recordRecordsWritten(UNKNOWN_TENANT_ID, jobId, count);
    }

    /** Records records committed to a Worker sink batch under a trusted tenant identity. */
    public void recordRecordsWritten(String tenantId, String jobId, long count) {
        recordRecordsWritten(tenantId, jobId, null, count);
    }

    /** Records written records with trusted identity and an optional request-id exemplar. */
    public void recordRecordsWritten(String tenantId, String jobId, String requestId, long count) {
        if (count <= 0) {
            return;
        }
        Labels exemplar = requestExemplar(requestId);
        if (prometheus != null) {
            increment(
                    prometheus.recordsWritten.labelValues(canonicalTenantId(tenantId), canonicalJobId(jobId)),
                    count,
                    exemplar);
            return;
        }
        increment("worker.records.written", tenantId, jobId, count);
    }

    /** Records sink records rejected with a stable reason code. */
    public void recordRecordsRejected(String jobId, String reason, long count) {
        recordRecordsRejected(UNKNOWN_TENANT_ID, jobId, reason, count);
    }

    /** Records rejected sink records with trusted tenant and stable reason labels. */
    public void recordRecordsRejected(String tenantId, String jobId, String reason, long count) {
        recordRecordsRejected(tenantId, jobId, reason, null, count);
    }

    /** Records rejected records with trusted identity and an optional request-id exemplar. */
    public void recordRecordsRejected(String tenantId, String jobId, String reason, String requestId, long count) {
        if (count <= 0) {
            return;
        }
        String stableReason = REJECTION_REASONS.contains(reason) ? reason : "UNKNOWN";
        Labels exemplar = requestExemplar(requestId);
        if (prometheus != null) {
            increment(
                    prometheus.recordsRejected.labelValues(
                            canonicalTenantId(tenantId), canonicalJobId(jobId), stableReason),
                    count,
                    exemplar);
            return;
        }
        Counter.builder("worker.records.rejected")
                .tags(commonTags(tenantId, jobId))
                .tag("reason", stableReason)
                .register(registry)
                .increment(count);
    }

    /** Records the configured batch size selected by the Coordinator. */
    public void recordBatchSize(String jobId, int batchSize) {
        recordBatchSize(UNKNOWN_TENANT_ID, jobId, batchSize);
    }

    /** Records the configured batch size with a trusted tenant identity. */
    public void recordBatchSize(String tenantId, String jobId, int batchSize) {
        recordBatchSize(tenantId, jobId, null, batchSize);
    }

    /** Records batch size with trusted identity and an optional request-id exemplar. */
    public void recordBatchSize(String tenantId, String jobId, String requestId, int batchSize) {
        if (batchSize <= 0) {
            return;
        }
        Labels exemplar = requestExemplar(requestId);
        if (prometheus != null) {
            observe(
                    prometheus.batchSize.labelValues(canonicalTenantId(tenantId), canonicalJobId(jobId)),
                    batchSize,
                    exemplar);
            return;
        }
        DistributionSummary.builder("coordinator.batch.size")
                .baseUnit("records")
                .tags(commonTags(tenantId, jobId))
                .register(registry)
                .record(batchSize);
    }

    /** Records a completed Coordinator batch duration under an allowlisted stage. */
    public void recordBatchDuration(String jobId, String stage, long durationNanos) {
        recordBatchDuration(UNKNOWN_TENANT_ID, jobId, stage, durationNanos);
    }

    /** Records a completed batch duration with trusted tenant identity and an allowlisted stage. */
    public void recordBatchDuration(String tenantId, String jobId, String stage, long durationNanos) {
        recordBatchDuration(tenantId, jobId, null, stage, durationNanos);
    }

    /** Records batch duration with trusted identity and an optional request-id exemplar. */
    public void recordBatchDuration(String tenantId, String jobId, String requestId, String stage, long durationNanos) {
        if (durationNanos < 0) {
            return;
        }
        String stableStage = BATCH_STAGES.contains(stage) ? stage : "unknown";
        Labels exemplar = requestExemplar(requestId);
        if (prometheus != null) {
            observe(
                    prometheus.batchDuration.labelValues(
                            canonicalTenantId(tenantId), canonicalJobId(jobId), stableStage),
                    Unit.nanosToSeconds(durationNanos),
                    exemplar);
            return;
        }
        recordTimer("coordinator.batch.duration", tenantId, jobId, durationNanos, "stage", stableStage);
    }

    /** Records durable checkpoint latency with a bounded outcome. */
    public void recordCheckpointDuration(String jobId, String outcome, long durationNanos) {
        recordCheckpointDuration(UNKNOWN_TENANT_ID, jobId, outcome, durationNanos);
    }

    /** Records durable checkpoint latency with trusted tenant identity and a bounded outcome. */
    public void recordCheckpointDuration(String tenantId, String jobId, String outcome, long durationNanos) {
        recordCheckpointDuration(tenantId, jobId, null, outcome, durationNanos);
    }

    /** Records checkpoint duration with trusted identity and an optional request-id exemplar. */
    public void recordCheckpointDuration(
            String tenantId, String jobId, String requestId, String outcome, long durationNanos) {
        if (durationNanos < 0) {
            return;
        }
        String stableOutcome = CHECKPOINT_OUTCOMES.contains(outcome) ? outcome : "failure";
        Labels exemplar = requestExemplar(requestId);
        if (prometheus != null) {
            observe(
                    prometheus.checkpointDuration.labelValues(
                            canonicalTenantId(tenantId), canonicalJobId(jobId), stableOutcome),
                    Unit.nanosToSeconds(durationNanos),
                    exemplar);
            return;
        }
        recordTimer("coordinator.checkpoint.duration", tenantId, jobId, durationNanos, "outcome", stableOutcome);
    }

    /** Records successfully enqueued Worker-local spill payload bytes. */
    public void recordSpillBytes(String jobId, long bytes) {
        recordSpillBytes(UNKNOWN_TENANT_ID, jobId, bytes);
    }

    /** Records successfully enqueued spill bytes with trusted tenant identity. */
    public void recordSpillBytes(String tenantId, String jobId, long bytes) {
        recordSpillBytes(tenantId, jobId, null, bytes);
    }

    /** Records spill bytes with trusted identity and an optional request-id exemplar. */
    public void recordSpillBytes(String tenantId, String jobId, String requestId, long bytes) {
        if (bytes <= 0) {
            return;
        }
        Labels exemplar = requestExemplar(requestId);
        if (prometheus != null) {
            increment(
                    prometheus.spillBytes.labelValues(canonicalTenantId(tenantId), canonicalJobId(jobId)),
                    bytes,
                    exemplar);
            return;
        }
        increment("coordinator.spill.bytes", tenantId, jobId, bytes);
    }

    private void increment(String name, String tenantId, String jobId, long count) {
        if (count <= 0) {
            return;
        }
        Counter.builder(name)
                .tags(commonTags(tenantId, jobId))
                .register(registry)
                .increment(count);
    }

    private void recordTimer(
            String name, String tenantId, String jobId, long durationNanos, String label, String value) {
        if (durationNanos < 0) {
            return;
        }
        Timer.builder(name)
                .tags(commonTags(tenantId, jobId))
                .tag(label, value)
                .register(registry)
                .record(durationNanos, TimeUnit.NANOSECONDS);
    }

    private static String[] commonTags(String tenantId, String jobId) {
        return new String[] {"tenant_id", canonicalTenantId(tenantId), "job_id", canonicalJobId(jobId)};
    }

    private static String canonicalTenantId(String tenantId) {
        return tenantId != null && CANONICAL_UUID.matcher(tenantId).matches() ? tenantId : UNKNOWN_TENANT_ID;
    }

    private static String canonicalJobId(String jobId) {
        return jobId != null && CANONICAL_UUID.matcher(jobId).matches() ? jobId : UNKNOWN_JOB_ID;
    }

    private static Labels requestExemplar(String requestId) {
        if (requestId == null || !CANONICAL_UUID.matcher(requestId).matches()) {
            return null;
        }
        return Labels.of("request_id", requestId);
    }

    private static void increment(CounterDataPoint counter, long count, Labels exemplar) {
        if (exemplar == null) {
            counter.inc(count);
        } else {
            counter.incWithExemplar(count, exemplar);
        }
    }

    private static void observe(DistributionDataPoint histogram, double value, Labels exemplar) {
        if (exemplar == null) {
            histogram.observe(value);
        } else {
            histogram.observeWithExemplar(value, exemplar);
        }
    }

    private static final class PrometheusBackend {
        private final io.prometheus.metrics.core.metrics.Counter recordsRead;
        private final io.prometheus.metrics.core.metrics.Counter recordsWritten;
        private final io.prometheus.metrics.core.metrics.Counter recordsRejected;
        private final io.prometheus.metrics.core.metrics.Counter spillBytes;
        private final io.prometheus.metrics.core.metrics.Histogram batchSize;
        private final io.prometheus.metrics.core.metrics.Histogram batchDuration;
        private final io.prometheus.metrics.core.metrics.Histogram checkpointDuration;

        private PrometheusBackend(PrometheusRegistry registry) {
            recordsRead = register(counter("worker.records.read", "tenant_id", "job_id"), registry);
            recordsWritten = register(counter("worker.records.written", "tenant_id", "job_id"), registry);
            recordsRejected = register(counter("worker.records.rejected", "tenant_id", "job_id", "reason"), registry);
            spillBytes = register(counter("coordinator.spill.bytes", "tenant_id", "job_id"), registry);
            batchSize = register(histogram("coordinator.batch.size", RECORDS, "tenant_id", "job_id"), registry);
            batchDuration = register(
                    histogram("coordinator.batch.duration", Unit.SECONDS, "tenant_id", "job_id", "stage"), registry);
            checkpointDuration = register(
                    histogram("coordinator.checkpoint.duration", Unit.SECONDS, "tenant_id", "job_id", "outcome"),
                    registry);
        }

        private static io.prometheus.metrics.core.metrics.Counter counter(String name, String... labelNames) {
            return io.prometheus.metrics.core.metrics.Counter.builder()
                    .name(name)
                    .labelNames(labelNames)
                    .withExemplars()
                    .build();
        }

        private static io.prometheus.metrics.core.metrics.Histogram histogram(
                String name, Unit unit, String... labelNames) {
            return io.prometheus.metrics.core.metrics.Histogram.builder()
                    .name(name)
                    .unit(unit)
                    .labelNames(labelNames)
                    .classicOnly()
                    .withExemplars()
                    .build();
        }

        private static io.prometheus.metrics.core.metrics.Counter register(
                io.prometheus.metrics.core.metrics.Counter counter, PrometheusRegistry registry) {
            registry.register(counter);
            return counter;
        }

        private static io.prometheus.metrics.core.metrics.Histogram register(
                io.prometheus.metrics.core.metrics.Histogram histogram, PrometheusRegistry registry) {
            registry.register(histogram);
            return histogram;
        }
    }
}
