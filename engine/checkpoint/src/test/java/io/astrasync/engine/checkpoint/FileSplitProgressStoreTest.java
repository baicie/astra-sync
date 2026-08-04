package io.astrasync.engine.checkpoint;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.kernel.SyncResult;
import io.astrasync.engine.runtime.WorkerResult;
import java.io.IOException;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.List;
import java.util.Map;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.io.TempDir;

class FileSplitProgressStoreTest {
    @TempDir
    Path directory;

    @Test
    void persistsAndReloadsFirstSuccessfulCompletion() throws IOException {
        SourceSplit first = split("split-1", "1");
        SourceSplit second = split("split-2", "2");
        SplitPlan plan = SplitPlan.from(List.of(first, second));
        FileSplitProgressStore store = new FileSplitProgressStore(directory);

        FullLoadProgress opened = store.open("orders-load", plan);
        FullLoadProgress completed =
                store.recordCompletion("orders-load", plan.fingerprint(), first, result("worker-a", first, 3));
        FullLoadProgress duplicate =
                store.recordCompletion("orders-load", plan.fingerprint(), first, result("worker-b", first, 99));
        FullLoadProgress reloaded =
                new FileSplitProgressStore(directory).load("orders-load").orElseThrow();

        assertThat(opened.completedSplits()).isEmpty();
        assertThat(completed.completedCount()).isEqualTo(1);
        assertThat(duplicate).isEqualTo(completed);
        assertThat(reloaded.completedResult("split-1").orElseThrow()).isEqualTo(result("worker-a", first, 3));
        assertThat(reloaded.isComplete()).isFalse();
        assertThat(Files.readString(directory.resolve("orders-load.json")))
                .contains("\"formatVersion\" : 1", "\"split-1\"")
                .doesNotContain("password");
        try (var files = Files.list(directory)) {
            assertThat(files.filter(path -> path.getFileName().toString().endsWith(".tmp")))
                    .isEmpty();
        }
    }

    @Test
    void rejectsChangedPlansDescriptorsAndResultIdentity() {
        SourceSplit first = split("split-1", "1");
        SplitPlan plan = SplitPlan.from(List.of(first));
        FileSplitProgressStore store = new FileSplitProgressStore(directory);
        store.open("orders-load", plan);

        assertThatThrownBy(() -> store.open("orders-load", SplitPlan.from(List.of(split("split-2", "2")))))
                .isInstanceOf(SplitPlanMismatchException.class)
                .hasMessage("split plan changed for job orders-load");
        assertThatThrownBy(() -> store.recordCompletion(
                        "orders-load", plan.fingerprint(), split("split-1", "9"), result("worker-a", first, 1)))
                .isInstanceOf(SplitPlanMismatchException.class)
                .hasMessage("split descriptor changed: split-1");
        assertThatThrownBy(() -> store.recordCompletion(
                        "orders-load", plan.fingerprint(), first, result("worker-a", split("another", "1"), 1)))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("Worker result changed split identity: another");
    }

    @Test
    void fingerprintsAreOrderIndependentAndRejectDuplicateIds() {
        SourceSplit first = split("split-1", "1");
        SourceSplit second = split("split-2", "2");

        assertThat(SplitPlan.from(List.of(first, second))).isEqualTo(SplitPlan.from(List.of(second, first)));
        assertThatThrownBy(() -> SplitPlan.from(List.of(first, first)))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("split id is duplicated: split-1");
    }

    @Test
    void reportsMissingAndCorruptProgressWithoutReplacingIt() throws IOException {
        SourceSplit split = split("split-1", "1");
        SplitPlan plan = SplitPlan.from(List.of(split));
        FileSplitProgressStore store = new FileSplitProgressStore(directory);

        assertThat(store.load("orders-load")).isEmpty();
        assertThatThrownBy(() ->
                        store.recordCompletion("orders-load", plan.fingerprint(), split, result("worker-a", split, 1)))
                .isInstanceOf(SplitProgressException.class)
                .hasMessage("split progress does not exist for job orders-load");

        Files.writeString(directory.resolve("orders-load.json"), "{not-json");
        assertThatThrownBy(() -> store.load("orders-load"))
                .isInstanceOf(SplitProgressException.class)
                .hasMessage("failed to read split progress orders-load.json");
        assertThat(Files.readString(directory.resolve("orders-load.json"))).isEqualTo("{not-json");
    }

    private static WorkerResult result(String workerId, SourceSplit split, long records) {
        return new WorkerResult(workerId, split.splitId(), new SyncResult(records, records, 1, (int) records, 7));
    }

    private static SourceSplit split(String splitId, String start) {
        return new SourceSplit(
                splitId, "jdbc:orders", new SplitPosition(Map.of("id", start)), SplitPosition.unbounded());
    }
}
