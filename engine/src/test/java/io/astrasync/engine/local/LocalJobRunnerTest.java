package io.astrasync.engine.local;

import static io.astrasync.connector.api.Capability.BATCH_READ;
import static io.astrasync.connector.api.Capability.BATCH_WRITE;
import static io.astrasync.connector.api.ConnectorRole.SINK;
import static io.astrasync.connector.api.ConnectorRole.SOURCE;
import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.ConnectorConfiguration;
import io.astrasync.connector.api.ConnectorDescriptor;
import io.astrasync.connector.api.ConnectorFactory;
import io.astrasync.connector.api.data.Row;
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
import io.astrasync.engine.plan.CompilationErrorCode;
import io.astrasync.engine.plan.ConnectorRegistry;
import io.astrasync.engine.plan.JobCompilationException;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Set;
import org.junit.jupiter.api.Test;

class LocalJobRunnerTest {
    @Test
    void cancellationBeforeMaterializationDoesNotCreateEitherConnector() {
        List<String> events = new ArrayList<>();
        ProbeFactory source = new ProbeFactory("source", SOURCE, events, RowBatch.end());
        ProbeFactory sink = new ProbeFactory("sink", SINK, events, null);

        assertThatThrownBy(() -> new LocalJobRunner(ConnectorRegistry.of(source, sink))
                        .run(jobSpec("source", "sink"), () -> true))
                .isInstanceOfSatisfying(io.astrasync.engine.kernel.SyncJobException.class, exception -> {
                    assertThat(exception.stage()).isEqualTo(io.astrasync.engine.kernel.SyncStage.CANCELLED);
                    assertThat(exception.partialResult().readCount()).isZero();
                });
        assertThat(events).isEmpty();
    }

    @Test
    void cancellationCheckFailureBeforeMaterializationIsStructured() {
        List<String> events = new ArrayList<>();
        ProbeFactory source = new ProbeFactory("source", SOURCE, events, RowBatch.end());
        ProbeFactory sink = new ProbeFactory("sink", SINK, events, null);

        assertThatThrownBy(() -> new LocalJobRunner(ConnectorRegistry.of(source, sink))
                        .run(jobSpec("source", "sink"), () -> {
                            throw new IllegalStateException("token boom");
                        }))
                .isInstanceOfSatisfying(io.astrasync.engine.kernel.SyncJobException.class, exception -> {
                    assertThat(exception.stage()).isEqualTo(io.astrasync.engine.kernel.SyncStage.CANCELLATION_CHECK);
                    assertThat(exception.getCause()).hasMessage("token boom");
                    assertThat(exception.partialResult()).isEqualTo(io.astrasync.engine.kernel.SyncResult.empty());
                });
        assertThat(events).isEmpty();
    }

    @Test
    void compilesBeforeCreatingAndCreatesBeforeOpening() {
        List<String> events = new ArrayList<>();
        ProbeFactory source = new ProbeFactory("source", SOURCE, events, RowBatch.last(List.of(Row.of("id", "1"))));
        ProbeFactory sink = new ProbeFactory("sink", SINK, events, null);

        LocalRunResult result = new LocalJobRunner(ConnectorRegistry.of(source, sink)).run(jobSpec("source", "sink"));

        assertThat(events)
                .containsExactly(
                        "source:create",
                        "sink:create",
                        "source:open",
                        "sink:open",
                        "source:read:2",
                        "sink:write:1",
                        "sink:close",
                        "source:close");
        assertThat(result.plan().jobName()).isEqualTo("local-runner");
        assertThat(result.metrics().readCount()).isEqualTo(1);
        assertThat(result.metrics().writtenCount()).isEqualTo(1);
    }

    @Test
    void compilationFailureCreatesNothing() {
        List<String> events = new ArrayList<>();
        ProbeFactory sink = new ProbeFactory("sink", SINK, events, null);

        assertThatThrownBy(() -> new LocalJobRunner(ConnectorRegistry.of(sink)).run(jobSpec("missing", "sink")))
                .isInstanceOfSatisfying(JobCompilationException.class, exception -> assertThat(exception.code())
                        .isEqualTo(CompilationErrorCode.CONNECTOR_NOT_FOUND));
        assertThat(events).isEmpty();
    }

    @Test
    void sourceCreationFailureDoesNotCreateOrOpenTheSink() {
        List<String> events = new ArrayList<>();
        ProbeFactory source = new ProbeFactory("source", SOURCE, events, RowBatch.end());
        source.createFailure = new IllegalArgumentException("invalid option 'path'");
        ProbeFactory sink = new ProbeFactory("sink", SINK, events, null);

        assertThatThrownBy(() -> new LocalJobRunner(ConnectorRegistry.of(source, sink)).run(jobSpec("source", "sink")))
                .isInstanceOfSatisfying(JobMaterializationException.class, exception -> {
                    assertThat(exception.role()).isEqualTo(SOURCE);
                    assertThat(exception.connector()).isEqualTo("source");
                    assertThat(exception).hasMessageContaining("invalid option 'path'");
                });
        assertThat(events).containsExactly("source:create");
    }

    @Test
    void sinkCreationFailureLeavesTheCreatedSourceUnopened() {
        List<String> events = new ArrayList<>();
        ProbeFactory source = new ProbeFactory("source", SOURCE, events, RowBatch.end());
        ProbeFactory sink = new ProbeFactory("sink", SINK, events, null);
        sink.createFailure = new IllegalArgumentException("invalid option 'path'");

        assertThatThrownBy(() -> new LocalJobRunner(ConnectorRegistry.of(source, sink)).run(jobSpec("source", "sink")))
                .isInstanceOfSatisfying(JobMaterializationException.class, exception -> {
                    assertThat(exception.role()).isEqualTo(SINK);
                    assertThat(exception.connector()).isEqualTo("sink");
                });
        assertThat(events).containsExactly("source:create", "sink:create");
    }

    @Test
    void nullFactoryProductIsAMaterializationFailureBeforeOpen() {
        List<String> events = new ArrayList<>();
        ProbeFactory source = new ProbeFactory("source", SOURCE, events, RowBatch.end());
        source.returnNull = true;
        ProbeFactory sink = new ProbeFactory("sink", SINK, events, null);

        assertThatThrownBy(() -> new LocalJobRunner(ConnectorRegistry.of(source, sink)).run(jobSpec("source", "sink")))
                .isInstanceOf(JobMaterializationException.class)
                .hasMessageContaining("null Source");
        assertThat(events).containsExactly("source:create");
    }

    private static JobSpec jobSpec(String source, String sink) {
        return new JobSpec(
                JobSpec.API_VERSION,
                JobSpec.KIND,
                new JobMetadata("local-runner"),
                new JobConfiguration(
                        new ConnectorSpec(source, Map.of("alpha", "first")),
                        List.of(),
                        new ConnectorSpec(sink, Map.of("beta", "second")),
                        new DeliverySpec(DeliveryGuarantee.AT_MOST_ONCE),
                        new RuntimeSpec(2)));
    }

    private static final class ProbeFactory implements ConnectorFactory {
        private final ConnectorDescriptor descriptor;
        private final List<String> events;
        private final RowBatch sourceBatch;
        private RuntimeException createFailure;
        private boolean returnNull;

        private ProbeFactory(
                String name, io.astrasync.connector.api.ConnectorRole role, List<String> events, RowBatch sourceBatch) {
            this.descriptor = new ConnectorDescriptor(
                    name, "1.0.0", Set.of(role), role == SOURCE ? Set.of(BATCH_READ) : Set.of(BATCH_WRITE));
            this.events = events;
            this.sourceBatch = sourceBatch;
        }

        @Override
        public ConnectorDescriptor descriptor() {
            return descriptor;
        }

        @Override
        public BatchSource createSource(ConnectorConfiguration configuration) {
            events.add(descriptor.name() + ":create");
            failCreationIfRequested();
            if (returnNull) {
                return null;
            }
            return new BatchSource() {
                @Override
                public void open() {
                    events.add(descriptor.name() + ":open");
                }

                @Override
                public RowBatch readBatch(int maxRows) {
                    events.add(descriptor.name() + ":read:" + maxRows);
                    return sourceBatch;
                }

                @Override
                public void close() {
                    events.add(descriptor.name() + ":close");
                }
            };
        }

        @Override
        public BatchSink createSink(ConnectorConfiguration configuration) {
            events.add(descriptor.name() + ":create");
            failCreationIfRequested();
            return new BatchSink() {
                @Override
                public void open() {
                    events.add(descriptor.name() + ":open");
                }

                @Override
                public void writeBatch(RowBatch batch) {
                    events.add(descriptor.name() + ":write:" + batch.size());
                }

                @Override
                public void close() {
                    events.add(descriptor.name() + ":close");
                }
            };
        }

        private void failCreationIfRequested() {
            if (createFailure != null) {
                throw createFailure;
            }
        }
    }
}
