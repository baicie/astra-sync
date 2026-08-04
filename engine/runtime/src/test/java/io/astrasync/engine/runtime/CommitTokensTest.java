package io.astrasync.engine.runtime;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import org.junit.jupiter.api.Test;

class CommitTokensTest {
    @Test
    void keepsTheLogicalBatchTokenStableAcrossExecutionEpochs() {
        String first = CommitTokens.forBatch("orders", "split-0", 1, "digest-a");
        String retry = CommitTokens.forBatch("orders", "split-0", 1, "digest-a");
        String changed = CommitTokens.forBatch("orders", "split-0", 1, "digest-b");

        assertThat(first).isEqualTo(retry).hasSize(64);
        assertThat(changed).isNotEqualTo(first);
    }

    @Test
    void rejectsIncompleteLogicalIdentity() {
        assertThatThrownBy(() -> CommitTokens.forBatch("orders", "split-0", 0, "digest"))
                .isInstanceOf(IllegalArgumentException.class);
    }
}
