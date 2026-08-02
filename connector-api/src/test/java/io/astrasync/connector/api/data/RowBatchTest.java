package io.astrasync.connector.api.data;

import static org.assertj.core.api.Assertions.assertThat;
import static org.assertj.core.api.Assertions.assertThatThrownBy;

import java.util.ArrayList;
import java.util.List;
import org.junit.jupiter.api.Test;

class RowBatchTest {
    private static final Row ROW = Row.of("id", 1);

    @Test
    void dataBatchIsNonTerminalAndDefensivelyCopiesRows() {
        List<Row> source = new ArrayList<>(List.of(ROW));

        RowBatch batch = RowBatch.data(source);
        source.clear();

        assertThat(batch.rows()).containsExactly(ROW);
        assertThat(batch.size()).isEqualTo(1);
        assertThat(batch.endOfInput()).isFalse();
        assertThatThrownBy(() -> batch.rows().add(Row.empty())).isInstanceOf(UnsupportedOperationException.class);
    }

    @Test
    void lastBatchMayContainRows() {
        RowBatch batch = RowBatch.last(List.of(ROW));

        assertThat(batch.rows()).containsExactly(ROW);
        assertThat(batch.endOfInput()).isTrue();
    }

    @Test
    void emptyLastBatchUsesTheSharedTerminalBatch() {
        assertThat(RowBatch.last(List.of())).isSameAs(RowBatch.end());
        assertThat(RowBatch.end().rows()).isEmpty();
        assertThat(RowBatch.end().size()).isZero();
        assertThat(RowBatch.end().endOfInput()).isTrue();
    }

    @Test
    void rejectsEmptyNonTerminalBatch() {
        assertThatThrownBy(() -> RowBatch.data(List.of()))
                .isInstanceOf(IllegalArgumentException.class)
                .hasMessageContaining("empty batch");
    }

    @Test
    void rejectsNullListAndNullRows() {
        assertThatThrownBy(() -> RowBatch.data(null))
                .isInstanceOf(NullPointerException.class)
                .hasMessageContaining("rows");
        assertThatThrownBy(() -> RowBatch.last(null))
                .isInstanceOf(NullPointerException.class)
                .hasMessageContaining("rows");
        assertThatThrownBy(() -> RowBatch.data(java.util.Arrays.asList(ROW, null)))
                .isInstanceOf(NullPointerException.class)
                .hasMessageContaining("null");
    }

    @Test
    void hasValueEquality() {
        RowBatch first = RowBatch.last(List.of(ROW));
        RowBatch second = RowBatch.last(List.of(Row.of("id", 1)));

        assertThat(first).isEqualTo(second).hasSameHashCodeAs(second);
        assertThat(first).isNotEqualTo(RowBatch.data(List.of(ROW)));
        assertThat(first.toString()).contains("endOfInput=true");
    }
}
