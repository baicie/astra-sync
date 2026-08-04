package io.astrasync.engine.runtime;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.data.Row;
import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.source.SplitPosition;
import java.util.Map;
import org.junit.jupiter.api.Test;

class CheckpointRuntimeContractsTest {
    @Test
    void epochFenceKeepsTheNewestEpochPerJobAndRejectsStaleExecutions() {
        EpochFence fence = new EpochFence();

        fence.activate("orders", 3);
        fence.activate("orders", 3);
        fence.activate("customers", 2);

        assertThat(fence.activeEpoch("orders")).isEqualTo(3);
        assertThat(fence.activeEpoch("customers")).isEqualTo(2);
        assertThat(fence.activeEpoch()).isEqualTo(3);
        fence.assertCurrent("orders", 3);

        assertThatThrownBy(() -> fence.activate("orders", 2))
                .isInstanceOf(EpochFencedException.class)
                .hasMessage("execution epoch 2 is stale; active epoch is 3");
        assertThatThrownBy(() -> fence.assertCurrent("orders", 2))
                .isInstanceOf(EpochFencedException.class)
                .hasMessage("execution epoch 2 is no longer active for job orders");
        assertThatThrownBy(() -> fence.assertCurrent("unknown", 1)).isInstanceOf(EpochFencedException.class);
    }

    @Test
    void checkpointContextAndProgressCarryIdentityAndStableFingerprints() {
        EpochFence fence = new EpochFence();
        fence.activate("orders", 4);
        SplitPosition position = new SplitPosition(Map.of("id", "12"));
        CheckpointExecutionContext context =
                new CheckpointExecutionContext("orders", 4, "split-1", "fingerprint", 2, position, fence);

        context.assertCurrent();
        assertThat(context.sourcePosition()).isEqualTo(position);
        assertThat(new CheckpointExecutionContext("orders", 4, "split-1", 2, position, fence).splitFingerprint())
                .isEqualTo("unknown");

        CheckpointProgress progress =
                new CheckpointProgress("orders", 4, "split-1", 3, position, "commit-3", "batch-digest");
        assertThat(progress.fingerprint()).hasSize(64).isEqualTo(progress.fingerprint());
        assertThat(new CheckpointProgress("orders", 4, "split-1", 3, position, "commit-4", "batch-digest")
                        .fingerprint())
                .isNotEqualTo(progress.fingerprint());
    }

    @Test
    void checkpointContractsRejectInvalidIdentityAndPositions() {
        EpochFence fence = new EpochFence();
        SplitPosition position = SplitPosition.unbounded();

        assertThatThrownBy(() -> new CheckpointExecutionContext("", 1, "split", 0, position, fence))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new CheckpointExecutionContext("job", 0, "split", 0, position, fence))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new CheckpointExecutionContext("job", 1, "split", -1, position, fence))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new CheckpointExecutionContext("job", 1, "split", 0, null, fence))
                .isInstanceOf(NullPointerException.class);
        assertThatThrownBy(() -> new CheckpointExecutionContext("job", 1, "split", 0, position, null))
                .isInstanceOf(NullPointerException.class);

        assertThatThrownBy(() -> new CheckpointProgress("job", 0, "split", 1, position, "token", "digest"))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new CheckpointProgress("job", 1, "split", 0, position, "token", "digest"))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new CheckpointProgress("job", 1, "split", 1, position, "", "digest"))
                .isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new CheckpointProgress("job", 1, "split", 1, position, "token", ""))
                .isInstanceOf(IllegalArgumentException.class);
    }

    @Test
    void batchDigestIsStableAndIncludesBatchContents() {
        RowBatch first = RowBatch.data(java.util.List.of(Row.of("id", 1)));
        RowBatch same = RowBatch.data(java.util.List.of(Row.of("id", 1)));
        RowBatch different = RowBatch.data(java.util.List.of(Row.of("id", 2)));

        assertThat(BatchDigests.sha256(first)).hasSize(64).isEqualTo(BatchDigests.sha256(same));
        assertThat(BatchDigests.sha256(first)).isNotEqualTo(BatchDigests.sha256(different));
        assertThat(BatchDigests.sha256(RowBatch.end())).isNotEqualTo(BatchDigests.sha256(first));
    }

    @Test
    void epochFenceValidatesActivationInputs() {
        EpochFence fence = new EpochFence();

        assertThatThrownBy(() -> fence.activate("", 1)).isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> fence.activate("orders", 0)).isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> fence.activeEpoch(" ")).isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> fence.assertCurrent(null, 1)).isInstanceOf(NullPointerException.class);
    }
}
