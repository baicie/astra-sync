package io.astrasync.engine.runtime;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import org.junit.jupiter.api.Test;

class AdaptiveBatchControllerTest {
    @Test
    void fixedPolicyPreservesTheConfiguredLimit() {
        AdaptiveBatchController controller = new AdaptiveBatchController(AdaptiveBatchPolicy.fixed(8), 8);

        controller.observe(new AdaptiveBatchSample(8, 10_000_000, 10_000_000, 1, 1));

        assertThat(controller.currentBatchRecords()).isEqualTo(8);
        assertThat(controller.ewmaNanos()).isEqualTo(-1);
    }

    @Test
    void fastAndSlowSamplesStayWithinConfiguredBounds() {
        AdaptiveBatchController controller = new AdaptiveBatchController(AdaptiveBatchPolicy.adaptive(2, 4, 100, 0), 8);

        controller.observe(new AdaptiveBatchSample(4, 50, 0, 0, 1));
        assertThat(controller.currentBatchRecords()).isEqualTo(8);

        controller.observe(new AdaptiveBatchSample(8, 500, 0, 0, 1));
        assertThat(controller.currentBatchRecords()).isEqualTo(4);
        controller.observe(new AdaptiveBatchSample(4, 500, 0, 0, 1));
        assertThat(controller.currentBatchRecords()).isEqualTo(2);
    }

    @Test
    void pressureAndCooldownPreventRapidOscillation() {
        AdaptiveBatchController controller = new AdaptiveBatchController(AdaptiveBatchPolicy.adaptive(1, 8, 100, 2), 8);

        controller.observe(new AdaptiveBatchSample(8, 10, 1, 1, 1));
        assertThat(controller.currentBatchRecords()).isEqualTo(4);
        assertThat(controller.cooldownRemaining()).isEqualTo(2);

        controller.observe(new AdaptiveBatchSample(4, 10, 0, 0, 1));
        controller.observe(new AdaptiveBatchSample(4, 10, 0, 0, 1));
        assertThat(controller.currentBatchRecords()).isEqualTo(4);
        assertThat(controller.cooldownRemaining()).isZero();

        controller.observe(new AdaptiveBatchSample(4, 10, 1, 1, 1));
        assertThat(controller.currentBatchRecords()).isEqualTo(2);
    }

    @Test
    void rejectsInvalidPolicyAndSamples() {
        assertThatThrownBy(() -> new AdaptiveBatchPolicy(0, 1, 1, 0)).isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new AdaptiveBatchPolicy(2, 1, 1, 0)).isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new AdaptiveBatchSample(1, 1, 0, 2, 1)).isInstanceOf(IllegalArgumentException.class);
        assertThatThrownBy(() -> new AdaptiveBatchController(AdaptiveBatchPolicy.fixed(4), 2))
                .isInstanceOf(IllegalArgumentException.class);
    }
}
