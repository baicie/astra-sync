package io.astrasync.engine.coordinator;

import static org.assertj.core.api.Assertions.assertThat;

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
import io.astrasync.engine.checkpoint.FileCheckpointStore;
import io.astrasync.engine.runtime.CdcTask;
import io.astrasync.engine.worker.InProcessCdcWorker;
import java.nio.file.Path;
import java.time.Duration;
import java.util.List;
import java.util.Map;
import java.util.Optional;
import java.util.concurrent.atomic.AtomicBoolean;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class CheckpointCdcCoordinatorTest {
    @TempDir
    Path checkpointDirectory;

    @Test
    void restoresTheLastAcknowledgedOffsetInANewEpoch() {
        FileCheckpointStore store = new FileCheckpointStore(checkpointDirectory);
        CheckpointCdcCoordinator coordinator = new CheckpointCdcCoordinator(new InProcessCdcWorker("worker-0"), store);
        OneBatchSource firstSource = new OneBatchSource(batch(1));
        AtomicBoolean firstStop = new AtomicBoolean();
        firstSource.stop = firstStop;

        CdcRunResult first = coordinator.run("job", "mysql-cdc:shop", task(firstSource), firstStop::get);

        assertThat(first.executionEpoch()).isEqualTo(1);
        assertThat(first.checkpointSequence()).isEqualTo(1);
        assertThat(first.recovered()).isFalse();
        assertThat(firstSource.openedAt).isEqualTo(SplitPosition.unbounded());

        OneBatchSource secondSource = new OneBatchSource(batch(2));
        AtomicBoolean secondStop = new AtomicBoolean();
        secondSource.stop = secondStop;
        CdcRunResult second = coordinator.run("job", "mysql-cdc:shop", task(secondSource), secondStop::get);

        assertThat(second.executionEpoch()).isEqualTo(2);
        assertThat(second.checkpointSequence()).isEqualTo(2);
        assertThat(second.recovered()).isTrue();
        assertThat(secondSource.openedAt.offsets()).containsEntry("cursor", "1");
        assertThat(store.load("job", "cdc-0").orElseThrow().sourcePosition().offsets())
                .containsEntry("cursor", "2");
    }

    private static CdcTask task(CdcSource source) {
        return new CdcTask("cdc-0", source, new RecordingSink(), Duration.ofMillis(10));
    }

    private static CdcBatch batch(long sequence) {
        DataEvent event = new ImmutableDataEvent(
                "event-" + sequence,
                SourcePosition.of(
                        "position-" + sequence,
                        "source",
                        "db",
                        "db.table",
                        Map.of("pos", Long.toString(sequence)),
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

    private static final class OneBatchSource implements CdcSource {
        private CdcBatch batch;
        private SplitPosition openedAt;
        private AtomicBoolean stop;

        private OneBatchSource(CdcBatch batch) {
            this.batch = batch;
        }

        @Override
        public void openAt(SplitPosition resumePosition) {
            openedAt = resumePosition;
        }

        @Override
        public Optional<CdcBatch> poll(Duration timeout) {
            CdcBatch next = batch;
            batch = null;
            return Optional.ofNullable(next);
        }

        @Override
        public SplitPosition acknowledge(CdcBatch acknowledgedBatch) {
            stop.set(true);
            return new SplitPosition(Map.of("cursor", Long.toString(acknowledgedBatch.sequence())));
        }

        @Override
        public void close() {}
    }

    private static final class RecordingSink implements CdcSink {
        private String token;

        @Override
        public void open(CheckpointContext context) {}

        @Override
        public void writeBatch(CdcBatch batch, SinkCommitContext commitContext) {
            token = commitContext.commitToken();
        }

        @Override
        public String lastCommitToken() {
            return token;
        }

        @Override
        public void close() {}
    }
}
