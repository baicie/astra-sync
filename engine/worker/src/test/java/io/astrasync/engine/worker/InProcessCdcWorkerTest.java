package io.astrasync.engine.worker;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.CheckpointContext;
import io.astrasync.connector.api.RecordKey;
import io.astrasync.connector.api.SinkCommitContext;
import io.astrasync.connector.api.SourcePosition;
import io.astrasync.connector.api.TraceContext;
import io.astrasync.connector.api.data.CdcBatch;
import io.astrasync.connector.api.data.CdcPhase;
import io.astrasync.connector.api.data.DataEvent;
import io.astrasync.connector.api.data.ImmutableDataEvent;
import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.sink.CdcSink;
import io.astrasync.connector.api.source.CdcSource;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.runtime.CdcTask;
import io.astrasync.engine.runtime.CheckpointExecutionContext;
import io.astrasync.engine.runtime.EpochFence;
import java.time.Duration;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.concurrent.atomic.AtomicInteger;
import org.junit.jupiter.api.Test;

class InProcessCdcWorkerTest {
    @Test
    void commitsSinkThenAcknowledgesSourceThenReportsCheckpoint() {
        List<String> order = new ArrayList<>();
        FakeSource source = new FakeSource(List.of(batch(1), batch(2)), order);
        FakeSink sink = new FakeSink(order, false);
        AtomicInteger checkpoints = new AtomicInteger();
        CheckpointExecutionContext context = context(SplitPosition.unbounded(), 0);

        var result = new InProcessCdcWorker("worker-0")
                .executeCdc(
                        context,
                        new CdcTask("cdc-0", source, sink, Duration.ofMillis(10)),
                        progress -> {
                            order.add("checkpoint-" + progress.checkpointSequence());
                            checkpoints.incrementAndGet();
                        },
                        () -> checkpoints.get() == 2);

        assertThat(order).containsSubsequence("sink-1", "ack-1", "checkpoint-1", "sink-2", "ack-2", "checkpoint-2");
        assertThat(result.metrics().readCount()).isEqualTo(2);
        assertThat(result.metrics().writtenCount()).isEqualTo(2);
        assertThat(result.metrics().batchCount()).isEqualTo(2);
        assertThat(source.openedAt).isEqualTo(SplitPosition.unbounded());
        assertThat(source.closed).isTrue();
        assertThat(sink.closed).isTrue();
    }

    @Test
    void doesNotAcknowledgeSourceWhenSinkCommitFails() {
        List<String> order = new ArrayList<>();
        FakeSource source = new FakeSource(List.of(batch(1)), order);
        FakeSink sink = new FakeSink(order, true);

        assertThatThrownBy(() -> new InProcessCdcWorker("worker-0")
                        .executeCdc(
                                context(SplitPosition.unbounded(), 0),
                                new CdcTask("cdc-0", source, sink, Duration.ofMillis(10)),
                                progress -> {},
                                () -> false))
                .isInstanceOfSatisfying(CdcTaskException.class, exception -> {
                    assertThat(exception.metrics().readCount()).isEqualTo(1);
                    assertThat(exception.metrics().writtenCount()).isZero();
                });
        assertThat(order).containsExactly("sink-1");
        assertThat(source.acknowledged).isZero();
        assertThat(source.closed).isTrue();
        assertThat(sink.closed).isTrue();
    }

    private static CheckpointExecutionContext context(SplitPosition position, long sequence) {
        EpochFence fence = new EpochFence();
        fence.activate("job", 1);
        return new CheckpointExecutionContext("job", 1, "cdc-0", sequence, position, fence);
    }

    private static CdcBatch batch(long sequence) {
        DataEvent event = new ImmutableDataEvent(
                "event-" + sequence,
                SourcePosition.of(
                        "position-" + sequence,
                        "source",
                        "db",
                        "db.table",
                        Map.of("source-pos", Long.toString(sequence)),
                        sequence,
                        "tx-" + sequence,
                        sequence),
                DataEvent.Operation.INSERT,
                sequence,
                sequence,
                "schema",
                "db.table",
                RecordKey.of(Map.of("id", sequence)),
                Row.empty(),
                Row.of(Map.of("id", sequence)),
                Map.of(),
                TraceContext.root());
        return new CdcBatch(sequence, List.of(event), CdcPhase.STREAMING, false);
    }

    private static final class FakeSource implements CdcSource {
        private final ArrayDeque<CdcBatch> batches;
        private final List<String> order;
        private SplitPosition openedAt;
        private int acknowledged;
        private boolean closed;

        private FakeSource(List<CdcBatch> batches, List<String> order) {
            this.batches = new ArrayDeque<>(batches);
            this.order = order;
        }

        @Override
        public void openAt(SplitPosition resumePosition) {
            openedAt = resumePosition;
        }

        @Override
        public Optional<CdcBatch> poll(Duration timeout) {
            return Optional.ofNullable(batches.poll());
        }

        @Override
        public SplitPosition acknowledge(CdcBatch batch) {
            acknowledged++;
            order.add("ack-" + batch.sequence());
            return new SplitPosition(Map.of("cursor", Long.toString(batch.sequence())));
        }

        @Override
        public void close() {
            closed = true;
        }
    }

    private static final class FakeSink implements CdcSink {
        private final List<String> order;
        private final boolean fail;
        private String token;
        private boolean closed;

        private FakeSink(List<String> order, boolean fail) {
            this.order = order;
            this.fail = fail;
        }

        @Override
        public void open(CheckpointContext context) {}

        @Override
        public void writeBatch(CdcBatch batch, SinkCommitContext commitContext) {
            order.add("sink-" + batch.sequence());
            if (fail) {
                throw new IllegalStateException("sink failure");
            }
            token = commitContext.commitToken();
        }

        @Override
        public String lastCommitToken() {
            return token;
        }

        @Override
        public void close() {
            closed = true;
        }
    }
}
