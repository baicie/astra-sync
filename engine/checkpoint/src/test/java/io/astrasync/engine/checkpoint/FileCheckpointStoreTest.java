package io.astrasync.engine.checkpoint;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.WorkerResult;
import java.nio.file.Path;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class FileCheckpointStoreTest {
    @TempDir
    Path directory;

    @Test
    void allocatesEpochsAndReloadsOrderedCheckpointRecords() {
        FileCheckpointStore store = new FileCheckpointStore(directory);
        assertThat(store.acquireEpoch("orders", plan("1"))).isEqualTo(1);
        CheckpointRecord first = record(1, 1, "1");

        assertThat(store.record(first)).isEqualTo(first);
        assertThat(new FileCheckpointStore(directory).load("orders", "split-0")).contains(first);
        assertThat(new FileCheckpointStore(directory).acquireEpoch("orders", plan("1")))
                .isEqualTo(2);
    }

    @Test
    void rejectsStaleEpochAndSequenceWithoutChangingDurableState() {
        FileCheckpointStore store = new FileCheckpointStore(directory);
        store.acquireEpoch("orders", plan("1"));
        CheckpointRecord first = record(1, 1, "1");
        store.record(first);
        store.acquireEpoch("orders", plan("1"));
        assertThatThrownBy(() -> store.record(record(1, 2, "2")))
                .isInstanceOf(StaleCheckpointException.class)
                .hasMessageContaining("active epoch is 2");
        CheckpointRecord second = record(2, 2, "2");
        store.record(second);

        assertThatThrownBy(() -> store.record(record(1, 3, "3")))
                .isInstanceOf(StaleCheckpointException.class)
                .hasMessageContaining("stale");
        assertThatThrownBy(() -> store.record(record(2, 4, "4")))
                .isInstanceOf(StaleCheckpointException.class)
                .hasMessageContaining("sequence");
        assertThat(store.load("orders", "split-0")).contains(second);
    }

    @Test
    void persistsCompletionAndRejectsPlanDriftBeforeAllocatingANewEpoch() {
        FileCheckpointStore store = new FileCheckpointStore(directory);
        SplitPlan plan = plan("1");
        long epoch = store.acquireEpoch("orders", plan);
        CheckpointRecord checkpoint = record(epoch, 1, "2");
        store.record(checkpoint);
        CheckpointCompletion completion = new CheckpointCompletion(
                "orders",
                epoch,
                "split-0",
                fingerprint(plan),
                1,
                new WorkerResult("worker-a", "split-0", new SyncResult(2, 2, 1, 2, 7)));

        assertThat(store.recordCompletion(completion)).isEqualTo(completion);
        assertThat(new FileCheckpointStore(directory).loadCompletion("orders", "split-0"))
                .contains(completion);
        assertThatThrownBy(() -> store.record(record(epoch, 2, "3")))
                .isInstanceOf(StaleCheckpointException.class)
                .hasMessageContaining("already complete");
        assertThatThrownBy(() -> store.acquireEpoch("orders", plan("9")))
                .isInstanceOf(SplitPlanMismatchException.class)
                .hasMessage("split plan changed for job orders");
    }

    private CheckpointRecord record(long epoch, long sequence, String position) {
        return new CheckpointRecord(
                "orders",
                epoch,
                "split-0",
                fingerprint(plan("1")),
                sequence,
                new SplitPosition(Map.of("id", position)),
                "commit-" + sequence,
                "digest-" + sequence);
    }

    private static SplitPlan plan(String start) {
        return SplitPlan.from(java.util.List.of(new SourceSplit(
                "split-0", "jdbc:orders", new SplitPosition(Map.of("id", start)), SplitPosition.unbounded())));
    }

    private static String fingerprint(SplitPlan plan) {
        return plan.splitFingerprints().get("split-0");
    }
}
