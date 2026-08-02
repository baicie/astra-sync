package io.astrasync.engine.kernel;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.ArrayList;
import java.util.List;
import org.junit.jupiter.api.Test;

class SyncBatchTest {
    @Test
    void defensivelyCopiesRecordsAndCarriesEndOfInput() {
        List<SyncRecord> records = new ArrayList<>();
        records.add(SyncRecord.of("id", 1));

        SyncBatch batch = SyncBatch.last(records);
        records.add(SyncRecord.of("id", 2));

        assertThat(batch.records()).hasSize(1);
        assertThat(batch.endOfInput()).isTrue();
        assertThatThrownBy(() -> batch.records().add(SyncRecord.of("id", 3)))
                .isInstanceOf(UnsupportedOperationException.class);
    }

    @Test
    void rejectsAnEmptyNonTerminalBatch() {
        assertThatThrownBy(() -> SyncBatch.data(List.of()))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessage("an empty batch must end the input");
        assertThat(SyncBatch.end().records()).isEmpty();
        assertThat(SyncBatch.end().endOfInput()).isTrue();
    }

    @Test
    void rejectsNullRecords() {
        List<SyncRecord> records = new ArrayList<>();
        records.add(null);

        assertThatThrownBy(() -> SyncBatch.last(records)).isInstanceOf(NullPointerException.class);
    }
}
