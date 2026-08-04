package io.astrasync.engine.network;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatCode;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import io.astrasync.connector.api.data.RowBatch;
import io.astrasync.connector.api.source.SourceSplit;
import io.astrasync.connector.api.source.SplitPosition;
import io.astrasync.engine.runtime.BatchTask;
import java.util.Map;
import org.junit.jupiter.api.Test;

class RemoteTaskFactoryTest {
    @Test
    void createsDescriptorOnlyTasksWithConfiguredLimits() {
        SourceSplit split = split();

        BatchTask task = new RemoteTaskFactory(128, 3).create(split);

        assertThat(task.split()).isEqualTo(split);
        assertThat(task.maxBatchRecords()).isEqualTo(128);
        assertThat(task.maxInFlightBatches()).isEqualTo(3);
        assertThatThrownBy(task.source()::open)
                .isInstanceOf(IllegalStateException.class)
                .hasMessage("remote descriptor source must not be opened on the Coordinator");
        assertThatThrownBy(() -> task.source().readBatch(1))
                .isInstanceOf(IllegalStateException.class)
                .hasMessage("remote descriptor source must not be read on the Coordinator");
        assertThatThrownBy(task.sink()::open)
                .isInstanceOf(IllegalStateException.class)
                .hasMessage("remote descriptor sink must not be opened on the Coordinator");
        assertThatThrownBy(() -> task.sink().writeBatch(RowBatch.end()))
                .isInstanceOf(IllegalStateException.class)
                .hasMessage("remote descriptor sink must not be written on the Coordinator");
        assertThatCode(task.source()::close).doesNotThrowAnyException();
        assertThatCode(task.sink()::close).doesNotThrowAnyException();
    }

    @Test
    void rejectsInvalidLimitsAndNullSplits() {
        assertThatThrownBy(() -> new RemoteTaskFactory(0, 1))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("maxBatchRecords must be positive");
        assertThatThrownBy(() -> new RemoteTaskFactory(1, 0))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("maxInFlightBatches must be positive");
        assertThatThrownBy(() -> new RemoteTaskFactory(1, 1).create(null))
                .isInstanceOf(NullPointerException.class)
                .hasMessage("split must not be null");
    }

    private static SourceSplit split() {
        return new SourceSplit(
                "split-1", "jdbc:SOURCE_DATA", new SplitPosition(Map.of("ID", "1")), SplitPosition.unbounded());
    }
}
