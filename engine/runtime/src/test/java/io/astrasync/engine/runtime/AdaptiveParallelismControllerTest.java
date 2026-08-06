package io.astrasync.engine.runtime;

import static org.assertj.core.api.Assertions.assertThat;

import org.junit.jupiter.api.Test;

class AdaptiveParallelismControllerTest {
    @Test
    void scalesUpOnlyWithBacklogAndClampsAtAvailableWorkers() {
        AdaptiveParallelismController controller =
                new AdaptiveParallelismController(AdaptiveParallelismPolicy.adaptive(1, 2, 8, 100, 0), 3);

        controller.observe(new AdaptiveParallelismSample(50, 2, 2));
        assertThat(controller.currentParallelism()).isEqualTo(3);
        controller.observe(new AdaptiveParallelismSample(50, 2, 3));
        assertThat(controller.currentParallelism()).isEqualTo(3);

        AdaptiveParallelismController noBacklog =
                new AdaptiveParallelismController(AdaptiveParallelismPolicy.adaptive(1, 2, 3, 100, 0), 3);
        noBacklog.observe(new AdaptiveParallelismSample(50, 0, 2));
        assertThat(noBacklog.currentParallelism()).isEqualTo(2);
    }

    @Test
    void slowsDownToTheMinimumAfterHighLatency() {
        AdaptiveParallelismController controller =
                new AdaptiveParallelismController(AdaptiveParallelismPolicy.adaptive(1, 3, 3, 100, 0), 3);

        controller.observe(new AdaptiveParallelismSample(200, 2, 3));
        controller.observe(new AdaptiveParallelismSample(200, 2, 2));
        controller.observe(new AdaptiveParallelismSample(200, 2, 1));

        assertThat(controller.currentParallelism()).isEqualTo(1);
    }

    @Test
    void disabledPolicyDoesNotChange() {
        AdaptiveParallelismController controller =
                new AdaptiveParallelismController(AdaptiveParallelismPolicy.fixed(2), 4);

        controller.observe(new AdaptiveParallelismSample(1_000_000, 10, 2));

        assertThat(controller.currentParallelism()).isEqualTo(2);
        assertThat(controller.ewmaNanos()).isEqualTo(-1);
    }
}
