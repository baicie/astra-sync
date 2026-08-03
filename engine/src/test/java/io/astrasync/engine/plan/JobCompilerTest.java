package io.astrasync.engine.plan;

import static io.astrasync.connector.api.Capability.BATCH_READ;
import static io.astrasync.connector.api.Capability.BATCH_WRITE;
import static io.astrasync.connector.api.Capability.REPLAYABLE_OFFSET;
import static io.astrasync.connector.api.Capability.STREAM_READ;
import static io.astrasync.connector.api.Capability.TRANSACTIONAL_COMMIT;
import static io.astrasync.connector.api.ConnectorRole.SINK;
import static io.astrasync.connector.api.ConnectorRole.SOURCE;
import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.Capability;
import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.ConnectorDescriptor;
import io.astrasync.connector.api.ConnectorFactory;
import io.astrasync.connector.api.ConnectorRole;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.sink.BatchSink;
import io.astrasync.connector.api.source.BatchSource;
import io.astrasync.engine.jobspec.ConnectorSpec;
import io.astrasync.engine.jobspec.DeliveryGuarantee;
import io.astrasync.engine.jobspec.DeliverySpec;
import io.astrasync.engine.jobspec.JobConfiguration;
import io.astrasync.engine.jobspec.JobMetadata;
import io.astrasync.engine.jobspec.JobSpec;
import io.astrasync.engine.jobspec.RuntimeSpec;
import io.astrasync.engine.jobspec.TransformSpec;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;

class JobCompilerTest {
    @Test
    void compilesAValueOnlyDeterministicPlan() {
        ProbeFactory source = probe("source", Set.of(SOURCE), Set.of(BATCH_READ));
        ProbeFactory sink = probe("sink", Set.of(SINK), Set.of(BATCH_WRITE));
        JobSpec jobSpec = jobSpec(
                "source", Map.of("zeta", "last", "alpha", "first"), "sink", Map.of(), DeliveryGuarantee.AT_MOST_ONCE);

        CompiledJobPlan first = new JobCompiler(ConnectorRegistry.of(source, sink)).compile(jobSpec);
        CompiledJobPlan second = new JobCompiler(ConnectorRegistry.of(sink, source)).compile(jobSpec);

        assertThat(first).isEqualTo(second);
        assertThat(first.jobName()).isEqualTo("compile-test");
        assertThat(first.source().options().keySet()).containsExactly("alpha", "zeta");
        assertThat(first.deliveryGuarantee()).isEqualTo(DeliveryGuarantee.AT_MOST_ONCE);
        assertThat(first.maxBatchRecords()).isEqualTo(32);
        assertThat(first.toString()).contains("alpha", "zeta").doesNotContain("first", "last");
        assertThat(source.createCount).hasValue(0);
        assertThat(sink.createCount).hasValue(0);
        assertThat(source.openCount).hasValue(0);
        assertThat(sink.openCount).hasValue(0);
    }

    @Test
    void rejectsMissingConnectorWrongRoleAndMissingBatchCapability() {
        ProbeFactory sinkOnly = probe("sink-only", Set.of(SINK), Set.of(BATCH_WRITE));
        ProbeFactory streamOnly = probe("stream-only", Set.of(SOURCE), Set.of(STREAM_READ));
        ProbeFactory sink = probe("sink", Set.of(SINK), Set.of(BATCH_WRITE));
        ConnectorRegistry registry = ConnectorRegistry.of(sinkOnly, streamOnly, sink);
        JobCompiler compiler = new JobCompiler(registry);

        assertCompilationFailure(
                () -> compiler.compile(jobSpec("missing", Map.of(), "sink", Map.of(), DeliveryGuarantee.AT_MOST_ONCE)),
                CompilationErrorCode.CONNECTOR_NOT_FOUND);
        assertCompilationFailure(
                () -> compiler.compile(
                        jobSpec("sink-only", Map.of(), "sink", Map.of(), DeliveryGuarantee.AT_MOST_ONCE)),
                CompilationErrorCode.ROLE_UNSUPPORTED);
        assertCompilationFailure(
                () -> compiler.compile(
                        jobSpec("stream-only", Map.of(), "sink", Map.of(), DeliveryGuarantee.AT_MOST_ONCE)),
                CompilationErrorCode.CAPABILITY_MISSING);

        assertThat(sinkOnly.createCount).hasValue(0);
        assertThat(streamOnly.createCount).hasValue(0);
        assertThat(sink.createCount).hasValue(0);
    }

    @Test
    void resolvesBothDescriptorsBeforeCheckingRolesAndCapabilities() {
        ProbeFactory streamOnly = probe("stream-only", Set.of(SOURCE), Set.of(STREAM_READ));
        JobCompiler compiler = new JobCompiler(ConnectorRegistry.of(streamOnly));

        assertCompilationFailure(
                () -> compiler.compile(
                        jobSpec("stream-only", Map.of(), "missing", Map.of(), DeliveryGuarantee.AT_MOST_ONCE)),
                CompilationErrorCode.CONNECTOR_NOT_FOUND);
        assertThat(streamOnly.createCount).hasValue(0);
        assertThat(streamOnly.openCount).hasValue(0);
    }

    @Test
    void rejectsTransformsInsteadOfIgnoringThem() {
        ProbeFactory source = probe("source", Set.of(SOURCE), Set.of(BATCH_READ));
        ProbeFactory sink = probe("sink", Set.of(SINK), Set.of(BATCH_WRITE));
        JobSpec base = jobSpec("source", Map.of(), "sink", Map.of(), DeliveryGuarantee.AT_MOST_ONCE);
        JobSpec transformed = new JobSpec(
                base.apiVersion(),
                base.kind(),
                base.metadata(),
                new JobConfiguration(
                        base.spec().source(),
                        List.of(new TransformSpec("mask", Map.of())),
                        base.spec().sink(),
                        base.spec().delivery(),
                        base.spec().runtime()));

        assertCompilationFailure(
                () -> new JobCompiler(ConnectorRegistry.of(source, sink)).compile(transformed),
                CompilationErrorCode.TRANSFORM_UNSUPPORTED);
        assertThat(source.createCount).hasValue(0);
        assertThat(sink.createCount).hasValue(0);
    }

    @Test
    void rejectsStrongerGuaranteesEvenWhenConnectorsAdvertiseFutureCapabilities() {
        ProbeFactory source = probe("source", Set.of(SOURCE), Set.of(BATCH_READ, REPLAYABLE_OFFSET));
        ProbeFactory sink = probe("sink", Set.of(SINK), Set.of(BATCH_WRITE, TRANSACTIONAL_COMMIT));
        JobCompiler compiler = new JobCompiler(ConnectorRegistry.of(source, sink));

        assertCompilationFailure(
                () -> compiler.compile(jobSpec("source", Map.of(), "sink", Map.of(), DeliveryGuarantee.AT_LEAST_ONCE)),
                CompilationErrorCode.DELIVERY_UNSUPPORTED);
        assertCompilationFailure(
                () -> compiler.compile(jobSpec("source", Map.of(), "sink", Map.of(), DeliveryGuarantee.EXACTLY_ONCE)),
                CompilationErrorCode.DELIVERY_UNSUPPORTED);

        assertThat(source.createCount).hasValue(0);
        assertThat(sink.createCount).hasValue(0);
        assertThat(source.openCount).hasValue(0);
        assertThat(sink.openCount).hasValue(0);
    }

    private static JobSpec jobSpec(
            String source,
            Map<String, String> sourceOptions,
            String sink,
            Map<String, String> sinkOptions,
            DeliveryGuarantee guarantee) {
        return new JobSpec(
                JobSpec.API_VERSION,
                JobSpec.KIND,
                new JobMetadata("compile-test"),
                new JobConfiguration(
                        new ConnectorSpec(source, sourceOptions),
                        List.of(),
                        new ConnectorSpec(sink, sinkOptions),
                        new DeliverySpec(guarantee),
                        new RuntimeSpec(32)));
    }

    private static ProbeFactory probe(String name, Set<ConnectorRole> roles, Set<Capability> capabilities) {
        return new ProbeFactory(new ConnectorDescriptor(name, "1.0.0", roles, capabilities));
    }

    private static void assertCompilationFailure(Runnable operation, CompilationErrorCode code) {
        assertThatThrownBy(operation::run)
                .isInstanceOfSatisfying(JobCompilationException.class, exception -> assertThat(exception.code())
                        .isEqualTo(code));
    }

    private static final class ProbeFactory implements ConnectorFactory {
        private final ConnectorDescriptor descriptor;
        private final AtomicInteger createCount = new AtomicInteger();
        private final AtomicInteger openCount = new AtomicInteger();

        private ProbeFactory(ConnectorDescriptor descriptor) {
            this.descriptor = descriptor;
        }

        @Override
        public ConnectorDescriptor descriptor() {
            return descriptor;
        }

        @Override
        public BatchSource createSource(ConnectorConfiguration configuration) {
            createCount.incrementAndGet();
            return new BatchSource() {
                @Override
                public void open() {
                    openCount.incrementAndGet();
                }

                @Override
                public RowBatch readBatch(int maxRows) {
                    return RowBatch.end();
                }

                @Override
                public void close() {}
            };
        }

        @Override
        public BatchSink createSink(ConnectorConfiguration configuration) {
            createCount.incrementAndGet();
            return new BatchSink() {
                @Override
                public void open() {
                    openCount.incrementAndGet();
                }

                @Override
                public void writeBatch(RowBatch batch) {}

                @Override
                public void close() {}
            };
        }
    }
}
