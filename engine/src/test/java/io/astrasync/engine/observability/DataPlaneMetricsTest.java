package io.astrasync.engine.observability;

import static org.assertj.core.api.Assertions.assertThat;

import io.micrometer.core.instrument.simple.SimpleMeterRegistry;
import io.micrometer.prometheusmetrics.PrometheusConfig;
import io.micrometer.prometheusmetrics.PrometheusMeterRegistry;
import java.util.UUID;
import org.junit.jupiter.api.Test;

class DataPlaneMetricsTest {
    private static final String JOB_ID = "1f36d9c6-77a2-4e83-8c7d-dc59b9b94a52";
    private static final String TENANT_ID = "2f36d9c6-77a2-4e83-8c7d-dc59b9b94a53";
    private static final String REQUEST_ID = "3f36d9c6-77a2-4e83-8c7d-dc59b9b94a54";
    private static final String OPENMETRICS_CONTENT_TYPE = "application/openmetrics-text; version=1.0.0; charset=utf-8";

    @Test
    void recordsWorkerCountsWithBoundedLabels() {
        SimpleMeterRegistry registry = new SimpleMeterRegistry();
        DataPlaneMetrics metrics = new DataPlaneMetrics(registry);

        metrics.recordRecordsRead(JOB_ID, 3);
        metrics.recordRecordsWritten(JOB_ID, 2);
        metrics.recordRecordsRejected(JOB_ID, "SINK_WRITE", 1);

        assertThat(registry.get("worker.records.read").counter().count()).isEqualTo(3);
        assertThat(registry.get("worker.records.written").counter().count()).isEqualTo(2);
        assertThat(registry.get("worker.records.rejected")
                        .tag("reason", "SINK_WRITE")
                        .counter()
                        .count())
                .isEqualTo(1);
        assertThat(registry.get("worker.records.read").counter().getId().getTag("tenant_id"))
                .isEqualTo(DataPlaneMetrics.UNKNOWN_TENANT_ID);
        assertThat(registry.get("worker.records.read").counter().getId().getTag("job_id"))
                .isEqualTo(JOB_ID);
    }

    @Test
    void collapsesNonCanonicalJobIdentifiersToUnknown() {
        SimpleMeterRegistry registry = new SimpleMeterRegistry();
        DataPlaneMetrics metrics = new DataPlaneMetrics(registry);

        metrics.recordRecordsRead("orders-load", 1);
        metrics.recordRecordsRead(UUID.randomUUID().toString().toUpperCase(), 1);

        assertThat(registry.get("worker.records.read")
                        .tag("job_id", DataPlaneMetrics.UNKNOWN_JOB_ID)
                        .counter()
                        .count())
                .isEqualTo(2);
    }

    @Test
    void recordsTrustedTenantAndJobIdentifiers() {
        SimpleMeterRegistry registry = new SimpleMeterRegistry();
        DataPlaneMetrics metrics = new DataPlaneMetrics(registry);

        metrics.recordRecordsRead(TENANT_ID, JOB_ID, 3);

        assertThat(registry.get("worker.records.read")
                        .tag("tenant_id", TENANT_ID)
                        .tag("job_id", JOB_ID)
                        .counter()
                        .count())
                .isEqualTo(3);
    }

    @Test
    void collapsesNonCanonicalTenantIdentifiersToUnknown() {
        SimpleMeterRegistry registry = new SimpleMeterRegistry();
        DataPlaneMetrics metrics = new DataPlaneMetrics(registry);

        metrics.recordRecordsRead("tenant-a", JOB_ID, 1);
        metrics.recordRecordsRead(UUID.randomUUID().toString().toUpperCase(), JOB_ID, 1);

        assertThat(registry.get("worker.records.read")
                        .tag("tenant_id", DataPlaneMetrics.UNKNOWN_TENANT_ID)
                        .tag("job_id", JOB_ID)
                        .counter()
                        .count())
                .isEqualTo(2);
    }

    @Test
    void recordsSpillBytesWithBoundedLabels() {
        SimpleMeterRegistry registry = new SimpleMeterRegistry();
        DataPlaneMetrics metrics = new DataPlaneMetrics(registry);

        metrics.recordSpillBytes(JOB_ID, 128);
        metrics.recordSpillBytes("not-a-job", 64);

        assertThat(registry.get("coordinator.spill.bytes")
                        .tag("tenant_id", DataPlaneMetrics.UNKNOWN_TENANT_ID)
                        .tag("job_id", JOB_ID)
                        .counter()
                        .count())
                .isEqualTo(128);
        assertThat(registry.get("coordinator.spill.bytes")
                        .tag("job_id", DataPlaneMetrics.UNKNOWN_JOB_ID)
                        .counter()
                        .count())
                .isEqualTo(64);
    }

    @Test
    void ignoresZeroValueSpillBytes() {
        SimpleMeterRegistry registry = new SimpleMeterRegistry();
        DataPlaneMetrics metrics = new DataPlaneMetrics(registry);

        metrics.recordSpillBytes(JOB_ID, 0);

        assertThat(registry.find("coordinator.spill.bytes").counter()).isNull();
    }

    @Test
    void recordsCheckpointDurationsWithBoundedOutcomes() {
        SimpleMeterRegistry registry = new SimpleMeterRegistry();
        DataPlaneMetrics metrics = new DataPlaneMetrics(registry);

        metrics.recordCheckpointDuration(JOB_ID, "success", 1);
        metrics.recordCheckpointDuration(JOB_ID, "untrusted outcome", 1);

        assertThat(registry.get("coordinator.checkpoint.duration")
                        .tag("outcome", "success")
                        .timer()
                        .count())
                .isEqualTo(1);
        assertThat(registry.get("coordinator.checkpoint.duration")
                        .tag("outcome", "failure")
                        .timer()
                        .count())
                .isEqualTo(1);
    }

    @Test
    void recordsBatchDurationsWithBoundedStages() {
        SimpleMeterRegistry registry = new SimpleMeterRegistry();
        DataPlaneMetrics metrics = new DataPlaneMetrics(registry);

        metrics.recordBatchDuration(JOB_ID, "read", 1);
        metrics.recordBatchDuration(JOB_ID, "write", 1);
        metrics.recordBatchDuration(JOB_ID, "transform", 1);

        assertThat(registry.get("coordinator.batch.duration")
                        .tag("job_id", JOB_ID)
                        .tag("stage", "read")
                        .timer()
                        .count())
                .isEqualTo(1);
        assertThat(registry.get("coordinator.batch.duration")
                        .tag("job_id", JOB_ID)
                        .tag("stage", "write")
                        .timer()
                        .count())
                .isEqualTo(1);
        assertThat(registry.get("coordinator.batch.duration")
                        .tag("job_id", JOB_ID)
                        .tag("stage", "unknown")
                        .timer()
                        .count())
                .isEqualTo(1);
    }

    @Test
    void ignoresZeroValueCountersAndRejectsUnknownReasons() {
        SimpleMeterRegistry registry = new SimpleMeterRegistry();
        DataPlaneMetrics metrics = new DataPlaneMetrics(registry);

        metrics.recordRecordsRead(JOB_ID, 0);
        metrics.recordRecordsRejected(JOB_ID, "untrusted error detail", 1);

        assertThat(registry.find("worker.records.read").counter()).isNull();
        assertThat(registry.get("worker.records.rejected")
                        .tag("reason", "UNKNOWN")
                        .counter()
                        .count())
                .isEqualTo(1);
    }

    @Test
    void recordsCanonicalRequestIdAsPrometheusExemplar() {
        PrometheusMeterRegistry registry = new PrometheusMeterRegistry(PrometheusConfig.DEFAULT);
        DataPlaneMetrics metrics = new DataPlaneMetrics(registry);

        metrics.recordRecordsRead(TENANT_ID, JOB_ID, REQUEST_ID, 3);
        metrics.recordRecordsWritten(TENANT_ID, JOB_ID, REQUEST_ID, 2);
        metrics.recordRecordsRejected(TENANT_ID, JOB_ID, "SINK_WRITE", REQUEST_ID, 1);
        metrics.recordBatchSize(TENANT_ID, JOB_ID, REQUEST_ID, 2);
        metrics.recordBatchDuration(TENANT_ID, JOB_ID, REQUEST_ID, "read", 1);
        metrics.recordCheckpointDuration(TENANT_ID, JOB_ID, REQUEST_ID, "success", 1);
        metrics.recordSpillBytes(TENANT_ID, JOB_ID, REQUEST_ID, 128);

        String body = registry.scrape(OPENMETRICS_CONTENT_TYPE);

        assertExemplar(body, "worker_records_read_total", REQUEST_ID);
        assertExemplar(body, "worker_records_written_total", REQUEST_ID);
        assertExemplar(body, "worker_records_rejected_total", REQUEST_ID);
        assertExemplar(body, "coordinator_batch_size_records", REQUEST_ID);
        assertExemplar(body, "coordinator_batch_duration_seconds", REQUEST_ID);
        assertExemplar(body, "coordinator_checkpoint_duration_seconds", REQUEST_ID);
        assertExemplar(body, "coordinator_spill_bytes_total", REQUEST_ID);
        assertThat(body).doesNotContain("trace_id=");
    }

    @Test
    void omitsNonCanonicalRequestIdFromPrometheusExemplar() {
        PrometheusMeterRegistry registry = new PrometheusMeterRegistry(PrometheusConfig.DEFAULT);
        DataPlaneMetrics metrics = new DataPlaneMetrics(registry);

        metrics.recordRecordsRead(TENANT_ID, JOB_ID, "not-a-uuid", 3);

        String body = registry.scrape(OPENMETRICS_CONTENT_TYPE);

        assertThat(body).contains("worker_records_read_total");
        assertThat(body).doesNotContain("request_id=");
    }

    private static void assertExemplar(String metrics, String family, String requestId) {
        assertThat(metrics.lines().toList())
                .anySatisfy(line -> assertThat(line).contains(family).contains("request_id=\"" + requestId + "\""));
    }
}
